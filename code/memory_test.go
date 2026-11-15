package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type fakeDDB struct {
	put   map[string]types.AttributeValue
	query *dynamodb.QueryInput
	items []memItem // returned newest first, like the real query
}

func (f *fakeDDB) PutItem(_ context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.put = in.Item
	return &dynamodb.PutItemOutput{}, nil
}

func (f *fakeDDB) Query(_ context.Context, in *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	f.query = in
	out := &dynamodb.QueryOutput{}
	for _, it := range f.items {
		m, _ := attributevalue.MarshalMap(it)
		out.Items = append(out.Items, m)
	}
	return out, nil
}

func withFakeDDB(t *testing.T, f *fakeDDB) {
	t.Helper()
	ddbOnce.Do(func() {}) // skip real client init
	oldDB, oldTable := ddb, memoryTable
	ddb, memoryTable = f, "mem"
	t.Cleanup(func() { ddb, memoryTable = oldDB, oldTable })
}

func TestSaveMemorySetsWeekTTL(t *testing.T) {
	f := &fakeDDB{}
	withFakeDDB(t, f)
	now := time.Unix(1_800_000_000, 0)
	if err := saveMemory(context.Background(), "u1", "s1", "q", "a", now); err != nil {
		t.Fatal(err)
	}
	var got memItem
	if err := attributevalue.UnmarshalMap(f.put, &got); err != nil {
		t.Fatal(err)
	}
	want := memItem{User: "u1", TS: now.UnixMilli(), Session: "s1", Question: "q", Answer: "a", ExpiresAt: now.Unix() + 7*24*3600}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoadMemoryOldestFirst(t *testing.T) {
	f := &fakeDDB{items: []memItem{{TS: 3, Question: "c"}, {TS: 2, Question: "b"}, {TS: 1, Question: "a"}}}
	withFakeDDB(t, f)
	items, err := loadMemory(context.Background(), "u1", "s1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Question != "a" || items[2].Question != "c" {
		t.Fatalf("bad order: %+v", items)
	}
	if v := f.query.ExpressionAttributeValues[":s"].(*types.AttributeValueMemberS).Value; v != "s1" {
		t.Fatalf("current session not excluded: %q", v)
	}
}

func TestLoadMemoryNoUser(t *testing.T) {
	f := &fakeDDB{}
	withFakeDDB(t, f)
	if items, err := loadMemory(context.Background(), "", "s1", time.Now()); err != nil || items != nil || f.query != nil {
		t.Fatalf("expected no query, got items=%v err=%v", items, err)
	}
}

func TestMemoryPrompt(t *testing.T) {
	if memoryPrompt(nil) != "" {
		t.Fatal("empty memory must add nothing")
	}
	ts := time.Date(2026, 11, 14, 21, 30, 0, 0, userTZ).UnixMilli()
	p := memoryPrompt([]memItem{{TS: ts, Question: "capital de Australia", Answer: "Canberra."}})
	if !strings.Contains(p, "- sábado 14/11 21:30: P: capital de Australia | R: Canberra.") {
		t.Fatalf("unexpected prompt: %q", p)
	}
}
