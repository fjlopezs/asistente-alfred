"""Alfred: Alexa skill that answers with Claude."""
import logging
import os

import anthropic
import boto3

log = logging.getLogger()
log.setLevel(logging.INFO)

MODEL = os.environ.get("CLAUDE_MODEL", "claude-haiku-5-5")
SKILL_ID = os.environ.get("ALEXA_SKILL_ID", "")
API_KEY_PARAM = os.environ.get("API_KEY_PARAM", "/alfred/anthropic-api-key")
MAX_TURNS = 10  # user/assistant pairs kept in the Alexa session

SYSTEM = (
    "Eres Alfred, un mayordomo asistente de voz que responde a través de Alexa en español latinoamericano neutro. "
    "Tus respuestas se leen en voz alta: sé breve (2-4 frases), natural y directo. "
    "No uses markdown, listas, viñetas, emojis, URLs ni símbolos que no se puedan pronunciar. "
    "Si la pregunta requiere una respuesta larga, da un resumen y ofrece ampliar."
)

_client = None


def client():
    global _client
    if _client is None:
        key = boto3.client("ssm").get_parameter(Name=API_KEY_PARAM, WithDecryption=True)["Parameter"]["Value"]
        # Alexa waits ~8s for the skill; fail fast instead of retrying.
        _client = anthropic.Anthropic(api_key=key, timeout=7.0, max_retries=0)
    return _client


def speak(text, end=False, attrs=None, reprompt="¿Algo más?"):
    resp = {
        "outputSpeech": {"type": "PlainText", "text": text},
        "shouldEndSession": end,
    }
    if not end:
        resp["reprompt"] = {"outputSpeech": {"type": "PlainText", "text": reprompt}}
    return {"version": "1.0", "sessionAttributes": attrs or {}, "response": resp}


def ask_claude(question, history):
    messages = history + [{"role": "user", "content": question}]
    r = client().messages.create(
        model=MODEL,
        max_tokens=400,
        system=SYSTEM,
        thinking={"type": "disabled"},
        output_config={"effort": "low"},
        messages=messages,
    )
    if r.stop_reason == "refusal":
        return "Lo siento, señor, no puedo ayudarle con eso."
    text = "".join(b.text for b in r.content if b.type == "text").strip()
    return text or "Disculpe, no tengo respuesta para eso."


def handle_question(question, attrs):
    history = attrs.get("history", [])
    try:
        answer = ask_claude(question, history)
    except anthropic.APITimeoutError:
        return speak("Disculpe, tardé demasiado pensando. ¿Puede repetir la pregunta de forma más corta?", attrs=attrs)
    except anthropic.RateLimitError:
        return speak("Estoy recibiendo demasiadas peticiones. Inténtelo en un momento.", attrs=attrs)
    except anthropic.APIStatusError as e:
        log.error("Claude API error %s: %s", e.status_code, e.message)
        return speak("Hubo un problema al consultar a Claude.", attrs=attrs)
    except anthropic.APIConnectionError:
        log.exception("Claude connection error")
        return speak("No pude conectarme con Claude.", attrs=attrs)
    history = (history + [
        {"role": "user", "content": question},
        {"role": "assistant", "content": answer},
    ])[-2 * MAX_TURNS:]
    return speak(answer, attrs={"history": history})


def lambda_handler(event, context):
    app_id = event.get("session", {}).get("application", {}).get("applicationId") \
        or event.get("context", {}).get("System", {}).get("application", {}).get("applicationId")
    if SKILL_ID and app_id != SKILL_ID:
        raise ValueError("Invalid applicationId")

    req = event["request"]
    attrs = (event.get("session") or {}).get("attributes") or {}
    rtype = req["type"]
    log.info("request type=%s", rtype)

    if rtype == "LaunchRequest":
        return speak("A sus órdenes. ¿En qué puedo ayudarle?")
    if rtype == "SessionEndedRequest":
        return {"version": "1.0", "response": {}}
    if rtype != "IntentRequest":
        return speak("No entendí eso.", attrs=attrs)

    intent = req["intent"]["name"]
    if intent == "ChatIntent":
        q = (req["intent"].get("slots", {}).get("query", {}) or {}).get("value")
        if not q:
            return speak("¿Qué desea preguntarme?", attrs=attrs)
        return handle_question(q, attrs)
    if intent in ("AMAZON.StopIntent", "AMAZON.CancelIntent", "AMAZON.NavigateHomeIntent"):
        return speak("Hasta luego, señor.", end=True)
    if intent == "AMAZON.HelpIntent":
        return speak("Hágame cualquier pregunta, por ejemplo: pregunta cuál es la capital de Australia.", attrs=attrs)
    if intent == "AMAZON.FallbackIntent":
        return speak("No le entendí. Empiece con: pregunta, o dime, seguido de su duda.", attrs=attrs)
    return speak("No entendí eso.", attrs=attrs)
