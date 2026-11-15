package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // provided.al2023 has no zoneinfo

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Conversation memory in DynamoDB: one item per question/answer, expired by
// TTL after a week. Past sessions go into the system prompt; the current
// session is already in the message history.

const (
	memoryTTL     = 7 * 24 * time.Hour
	memoryLimit   = 20 // past Q/A pairs given to Claude
	memoryTimeout = time.Second
)

var (
	memoryTable = os.Getenv("MEMORY_TABLE")
	userTZ      = loadTZ(envOr("TIMEZONE", "America/Santiago"))

	ddbClient lazy[ddbAPI]
)

type ddbAPI interface {
	Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
}

type memItem struct {
	User      string `dynamodbav:"user"`
	TS        int64  `dynamodbav:"ts"` // unix millis
	Session   string `dynamodbav:"session"`
	Question  string `dynamodbav:"question"`
	Answer    string `dynamodbav:"answer"`
	ExpiresAt int64  `dynamodbav:"expires_at"` // unix seconds, DynamoDB TTL
}

func loadTZ(name string) *time.Location {
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.UTC
}

func getDDB(ctx context.Context) (ddbAPI, error) {
	return ddbClient.get(ctx, func(ctx context.Context) (ddbAPI, error) {
		cfg, err := awsConfig(ctx)
		if err != nil {
			return nil, err
		}
		return dynamodb.NewFromConfig(cfg), nil
	})
}

// loadMemory returns up to memoryLimit Q/A pairs from other sessions in the
// last week, oldest first. TTL deletion lags, so the key condition also
// filters by age.
func loadMemory(ctx context.Context, user, session string, now time.Time) ([]memItem, error) {
	if memoryTable == "" || user == "" {
		return nil, nil
	}
	db, err := getDDB(ctx)
	if err != nil {
		return nil, err
	}
	out, err := db.Query(ctx, &dynamodb.QueryInput{
		TableName:                aws.String(memoryTable),
		KeyConditionExpression:   aws.String("#u = :u AND ts >= :since"),
		FilterExpression:         aws.String("#s <> :s"),
		ExpressionAttributeNames: map[string]string{"#u": "user", "#s": "session"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":u":     &types.AttributeValueMemberS{Value: user},
			":s":     &types.AttributeValueMemberS{Value: session},
			":since": &types.AttributeValueMemberN{Value: fmt.Sprint(now.Add(-memoryTTL).UnixMilli())},
		},
		ScanIndexForward: aws.Bool(false), // newest first
		Limit:            aws.Int32(memoryLimit + 2*maxTurns),
	})
	if err != nil {
		return nil, err
	}
	var items []memItem
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &items); err != nil {
		return nil, err
	}
	if len(items) > memoryLimit {
		items = items[:memoryLimit]
	}
	slices.Reverse(items)
	return items, nil
}

func saveMemory(ctx context.Context, user, session, q, a string, now time.Time) error {
	if memoryTable == "" || user == "" {
		return nil
	}
	db, err := getDDB(ctx)
	if err != nil {
		return err
	}
	item, err := attributevalue.MarshalMap(memItem{
		User: user, TS: now.UnixMilli(), Session: session,
		Question: q, Answer: a, ExpiresAt: now.Add(memoryTTL).Unix(),
	})
	if err != nil {
		return err
	}
	_, err = db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(memoryTable), Item: item})
	return err
}

var weekdays = [...]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}

// memoryPrompt renders past conversations for the system prompt.
func memoryPrompt(items []memItem) string {
	if len(items) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\nConversaciones anteriores con el usuario (últimos 7 días, hora local). " +
		"Úsalas solo si vienen al caso, por ejemplo si se refiere a algo que preguntó antes; no las menciones si no:")
	for _, it := range items {
		t := time.UnixMilli(it.TS).In(userTZ)
		fmt.Fprintf(&sb, "\n- %s %s: P: %s | R: %s", weekdays[t.Weekday()], t.Format("02/01 15:04"), it.Question, it.Answer)
	}
	return sb.String()
}

// recall loads past conversations without ever failing the request.
func recall(ctx context.Context, user, session string) string {
	ctx, cancel := context.WithTimeout(ctx, memoryTimeout)
	defer cancel()
	items, err := loadMemory(ctx, user, session, time.Now())
	if err != nil {
		log.Printf("memory load: %v", err)
	}
	return memoryPrompt(items)
}

func remember(ctx context.Context, user, session, q, a string) {
	ctx, cancel := context.WithTimeout(ctx, memoryTimeout)
	defer cancel()
	if err := saveMemory(ctx, user, session, q, a, time.Now()); err != nil {
		log.Printf("memory save: %v", err)
	}
}
