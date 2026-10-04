---
compiled: 2025-01-01
origin: self
source_files:
  - raw/go-channel-三铁律.md
tags:
  - go
  - concurrency
  - channel
  - goroutine-leak
type: source
updated: 2026-10-03
---

# Go channel 并发三铁律(合成演示源)

## 一句话结论
Go channel 的 goroutine 泄漏源于"发送阻塞而接收方已退出";三铁律——谁创建谁关闭、发送时监听取消、错误与结果同通道——配合 `defer close`,系统性地消除泄漏。

## 论证链
1. **问题定位**:channel 是 goroutine 间通信原语,用错导致 goroutine 泄漏。核心场景:生产者发送时消费者已退出(或反之),发送永远阻塞 → 泄漏的 goroutine 不会 panic,而是慢慢吃光内存,比 bug 更难发现。
2. **铁律 1:谁创建,谁关闭**。关闭责任在生产者;发送循环结束后由生产者 `close`,不在消费者侧关闭;关闭已关闭的 channel 会 panic。
3. **铁律 2:发送时监听取消**。发送放在 `select` 里同时监听 `ctx.Done()`:

```go
select {
case ch <- v:
case <-ctx.Done():
    return // 消费者已退出,别阻塞泄漏
}
```

4. **铁律 3:错误也走同一个 channel**。结果和错误分两个 channel 会变成两条时间线,消费者需 select 两个通道并猜测顺序。用结构体打包:

```go
type Result struct {
    Data string
    Err  error
}
ch := make(chan Result, 1)
```

5. **配套:`defer close`**。生产者用 `defer close(ch)` 保证任何 return 路径都关闭 channel——最简单也最容易忘记的一条。

## 关键细节
- 关闭已关闭 channel → panic;关闭权必须收敛到生产者单点。
- `ctx.Done()` 与发送同置于 select,取消时直接 return,避免阻塞泄漏。
- 单一 `Result` 通道 + 缓冲 1(`make(chan Result, 1)`),消除"两条时间线"问题。
- 泄漏的症状:不崩溃、内存缓慢增长——诊断困难。

## 作者立场与定位
规范式/最佳实践总结(合成演示源),立场明确:泄漏是 Go 并发首要陷阱,三条铁律是防御性约定而非可选风格("铁律"级别,不容讨价还价)。

## 意外发现
- 原文指出错误与结果分双通道会造成"两条时间线",消费者要猜测顺序。联想到用户场景:一旦系统里出现多个结果/错误通道,不仅有序问题,还叠加铁律 1 的关闭责任问题(谁关哪个通道?)——双通道方案会把三铁律全部复杂化,单一 `Result` 通道是唯一低熵解。
- 原文说泄漏的 goroutine 不会崩、只会慢慢吃内存("比 bug 更难发现")——意味着用户场景中 goroutine 数 / 内存曲线缓慢爬升往往不是内存泄漏 bug,而是 channel 阻塞型泄漏;应在监控中加入 goroutine 数量指标(pprof goroutine profile),排查时先看两侧生命周期是否对称。

## 疑点
- 铁律 1 与 `defer close` 在"生产者提前 return(错误路径)"时是否仍满足语义?defer 能保证关闭,但消费者可能收到部分数据后 channel 被关——文中未讨论消费者侧的 range/ok 检测。
- `make(chan Result, 1)` 的缓冲大小未论证为何是 1。
- 铁律 2 中消费者退出后 `Err` 如何传递给调用方?原文未展开。
- 文中未覆盖 `nil channel`(select 中永久阻塞的另一种泄漏形式)。

## 术语
- channel / goroutine / close / select / defer:Go 并发原语(本文单源,不建概念页)
- `ctx.Done()`:context 取消信号
- goroutine 泄漏:发送/接收永久阻塞导致 goroutine 无法退出

## 连接
- 未见既有页面可链接(当前 wiki 无其他 Go/并发相关页面)。若后续引入 context 取消、pprof 诊断等源,应链接至对应概念页并注明"该页为本文铁律 X 的具体应用"。

## 引用
- 源:`raw/go-channel-三铁律.md`(examples/ 合成演示 raw);编译产物对照:`examples/compiled/source-demo.md`。
