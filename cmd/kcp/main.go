// kcp — 知识编译复利引擎:自研 agent,任意 LLM 可跑。
package main

import (
	"os"

	"github.com/knowledge-compounder/kcp/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
