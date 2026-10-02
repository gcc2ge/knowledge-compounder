"""Provider 抽象:任意 LLM 的统一决策器(M02 能力位 + M04 决策器)。

能力位 capabilities.tools:支持则 function calling,否则循环降级 ReAct。
实现:
  OpenAICompatible  — 同一协议覆盖 OpenAI/DeepSeek/Moonshot/OpenRouter/Ollama/vLLM/Gemini-OpenAI 端点
  Anthropic        — /v1/messages + tool_use/tool_result 块

骨架用 stdlib urllib,非流式;流式/重试/超时升级见 TODO。
"""

from __future__ import annotations

import json
import urllib.request
import urllib.error
from dataclasses import dataclass, field
from typing import Any, Callable


@dataclass
class ToolDef:
    name: str
    description: str
    parameters: dict
    func: Callable[[dict], str]


@dataclass
class Capabilities:
    tools: bool = True
    streaming: bool = False


@dataclass
class Message:
    role: str  # system | user | assistant | tool
    content: str = ""
    tool_calls: list | None = None  # [{id,name,arguments}]
    tool_call_id: str | None = None

    def as_dict(self) -> dict:
        if self.role == "assistant" and self.tool_calls:
            return {"role": "assistant", "content": self.content or None,
                    "tool_calls": [{"id": tc["id"], "type": "function",
                                    "function": {"name": tc["name"], "arguments": tc["arguments"]}}
                                   for tc in self.tool_calls]}
        if self.role == "tool":
            return {"role": "tool", "tool_call_id": self.tool_call_id, "content": self.content}
        return {"role": self.role, "content": self.content}


class Provider:
    name = "base"
    ENV_KEY = "KCP_API_KEY"

    def __init__(self, model: str, api_key: str | None = None, base_url: str | None = None):
        self.model = model
        self.api_key = api_key or ""
        self.base_url = (base_url or "").rstrip("/")

    @property
    def capabilities(self) -> Capabilities:
        return Capabilities()

    def chat(self, messages: list[Message], tools: list[ToolDef] | None = None, **kw) -> Message:
        raise NotImplementedError


def _http_json(url: str, payload: dict, headers: dict, timeout: int = 180) -> dict:
    req = urllib.request.Request(url, data=json.dumps(payload).encode(),
                                 headers=headers, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        body = e.read().decode(errors="ignore")[:1000]
        raise RuntimeError(f"LLM API {e.code}: {body}")


class OpenAICompatible(Provider):
    """覆盖 OpenAI / DeepSeek / Moonshot / OpenRouter / Ollama / vLLM / Gemini-OpenAI 端点。"""
    name = "openai-compatible"

    def chat(self, messages, tools=None, **kw):
        payload = {"model": self.model, "messages": [m.as_dict() for m in messages]}
        if tools:
            payload["tools"] = [
                {"type": "function", "function": {"name": t.name, "description": t.description,
                                                  "parameters": t.parameters}}
                for t in tools]
        if kw.get("stop"):
            payload["stop"] = kw["stop"]
        data = _http_json(f"{self.base_url}/chat/completions", payload,
                          {"Authorization": f"Bearer {self.api_key}", "Content-Type": "application/json"})
        msg = data["choices"][0]["message"]
        tcs = msg.get("tool_calls")
        parsed = [{"id": tc["id"], "name": tc["function"]["name"],
                   "arguments": tc["function"]["arguments"]} for tc in tcs] if tcs else None
        return Message(role="assistant", content=msg.get("content") or "", tool_calls=parsed)


class Anthropic(Provider):
    name = "anthropic"
    ENV_KEY = "ANTHROPIC_API_KEY"

    def chat(self, messages, tools=None, **kw):
        system = "\n".join(m.content for m in messages if m.role == "system")
        blocks = []
        for m in messages:
            if m.role == "system":
                continue
            if m.role == "assistant" and m.tool_calls:
                content = []
                if m.content:
                    content.append({"type": "text", "text": m.content})
                for tc in m.tool_calls:
                    content.append({"type": "tool_use", "id": tc["id"], "name": tc["name"],
                                    "input": json.loads(tc["arguments"] or "{}")})
                blocks.append({"role": "assistant", "content": content})
            elif m.role == "tool":
                blocks.append({"role": "user", "content": [
                    {"type": "tool_result", "tool_use_id": m.tool_call_id, "content": m.content}]})
            else:
                blocks.append({"role": m.role, "content": m.content})
        payload = {"model": self.model, "messages": blocks, "max_tokens": kw.get("max_tokens", 8192)}
        if tools:
            payload["tools"] = [{"name": t.name, "description": t.description,
                                 "input_schema": t.parameters} for t in tools]
        if kw.get("stop"):
            payload["stop_sequences"] = kw["stop"]
        data = _http_json(f"{self.base_url}/v1/messages", payload,
                          {"x-api-key": self.api_key, "anthropic-version": "2023-06-01",
                           "Content-Type": "application/json"})
        content = data["content"]
        tcs = [{"id": b["id"], "name": b["name"], "arguments": json.dumps(b.get("input", {}))}
               for b in content if b.get("type") == "tool_use"]
        text = "".join(b.get("text", "") for b in content if b.get("type") == "text")
        return Message(role="assistant", content=text, tool_calls=tcs or None)


def make_provider(provider: str | None = None, model: str | None = None,
                  api_key: str | None = None, base_url: str | None = None) -> Provider:
    import harness.config as cfg
    p = provider or cfg.PROVIDER
    m = model or cfg.MODEL
    key = api_key or cfg.API_KEY
    url = base_url or cfg.BASE_URL
    if p == "anthropic":
        return Anthropic(m, key, url or "https://api.anthropic.com")
    return OpenAICompatible(m, key, url or "https://api.openai.com/v1")
