"""环境配置:选择 provider / model / key / base_url。

优先级:环境变量 > 默认值。
兼容:OpenAI 兼容端点覆盖 OpenAI/DeepSeek/Moonshot/OpenRouter/Ollama/vLLM。
"""

import os

# Provider 选择: openai-compatible | anthropic
PROVIDER = os.environ.get("KCP_PROVIDER", "openai-compatible")
MODEL = os.environ.get("KCP_MODEL", "deepseek-chat")
BASE_URL = os.environ.get("KCP_BASE_URL", "")
API_KEY = os.environ.get("KCP_API_KEY", "")

# 常用预设(可直接覆盖环境变量)
PRESETS = {
    # provider: (base_url 前缀说明, 默认模型)
    "openai": ("https://api.openai.com/v1", "gpt-4o-mini"),
    "deepseek": ("https://api.deepseek.com/v1", "deepseek-chat"),
    "anthropic": ("https://api.anthropic.com", "claude-sonnet-4-5"),
    "ollama": ("http://localhost:11434/v1", "qwen2.5:7b"),
    "gemini-openai": ("https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.0-flash"),
}

MAX_STEPS = int(os.environ.get("KCP_MAX_STEPS", "10"))
MAX_SAME_ACTION = int(os.environ.get("KCP_MAX_SAME_ACTION", "3"))
