// kcp — 知识编译复利引擎:自研 agent,任意 LLM 可跑。
package main

import (
	"os"

	"github.com/gcc2ge/knowledge-compounder/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
