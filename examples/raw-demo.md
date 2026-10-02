# Go channel 并发三铁律(合成演示源)

> 本文件是 examples/ 的**合成演示** raw——虚构的一篇短文,用来展示 compiler 怎么把它编译成符合 SCHEMA 的源摘要页。编译产物见 `examples/compiled/source-demo.md`。

Go 的 channel 是 goroutine 之间通信的原语,但用错会造成 goroutine 泄漏。有三个铁律:

**铁律 1:谁创建,谁关闭。** channel 的关闭责任在生产者。生产者在发送循环结束后负责 `close`,不要在消费者侧关闭。关闭已关闭的 channel 会 panic。

**铁律 2:发送时监听取消。** 发送方要把发送放在 `select` 里,同时监听 `ctx.Done()`:

```go
select {
case ch <- v:
case <-ctx.Done():
    return // 消费者已退出,别阻塞泄漏
}
```

**铁律 3:错误也走同一个 channel。** 不要把结果和错误分开两个 channel 发——它们会变成两条时间线,消费者要 select 两个通道并猜测顺序。用一个结构体把结果和错误打包:

```go
type Result struct {
    Data string
    Err  error
}
ch := make(chan Result, 1)
```

**配套:defer close。** 生产者用 `defer close(ch)` 保证函数无论怎么 return 都关闭 channel,这是最简单也最容易忘记的一条。

这三个铁律解决的核心问题是:生产者发送时消费者已经退出(或反过来),发送会永远阻塞,goroutine 泄漏。泄漏的 goroutine 不会崩,但会慢慢吃光内存——比 bug 更难发现。
