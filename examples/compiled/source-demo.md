---
source_files:
  - examples/raw-demo.md
origin: external
compiled: 2026-10-02
type: source
tags: [演示, go, 并发, channel]
---
# Go channel 并发三铁律(合成演示编译产物)

> ⚠️ 本页是**合成演示**,目的是展示 SCHEMA 编译纪律长什么样。source 页是自包含知识单元——不看 raw 也能带走全部论证,包括代码。

## 一句话结论

Go channel 并发有三个铁律——生产者负责关闭、发送时 select 监听取消、错误走同一个 channel——它们共同解决"发送方阻塞导致 goroutine 泄漏"这一类最难发现的并发 bug。

## 论证链

1. **问题定性**:channel 是 goroutine 通信原语,但用错造成 goroutine 泄漏;泄漏的 goroutine 不崩,但慢慢吃光内存,比 bug 更难发现。
2. **铁律 1:谁创建谁关闭**。关闭责任在生产者,发送循环结束后 `close`;消费者侧关闭或重复关闭都会 panic。
3. **铁律 2:发送时监听取消**。发送方把发送放进 `select`,同时监听 `ctx.Done()`——消费者已退出时不阻塞、直接 return,防泄漏:
```go
select {
case ch <- v:
case <-ctx.Done():
    return // 消费者已退出,别阻塞泄漏
}
```
4. **铁律 3:错误走同一个 channel**。结果和错误分开两个 channel 会变成两条时间线,消费者要猜顺序;用结构体打包:
```go
type Result struct {
    Data string
    Err  error
}
ch := make(chan Result, 1)
```
5. **配套 defer close**:生产者 `defer close(ch)` 保证任何 return 路径都关闭——最简单也最容易忘记。

## 关键细节

- `ctx.Done()` 必须在 `select` 里与发送并列,不能单独 `if` 判断(发送仍可能阻塞)
- 打包结构体比双 channel 少一次消费者侧的 select 与顺序猜测
- channel 带缓冲(`make(chan T, N)`)可降低部分阻塞概率,但不改变三条铁律的责任边界

## 作者立场与定位

演示源以"工程经验法则"形式呈现,非论文;三铁律是社区广泛接受的 Go 并发纪律(M01 课程将其列为 channel 使用规范的核心)。适用于标准 goroutine 协作;不覆盖 select 多路复用以外的复杂同步场景。

## 意外发现

原文把"泄漏 goroutine 不崩但慢慢吃光内存"单独点出——这让我想到:泄漏 bug 在测试期几乎测不出来,只在长期运行的压力下浮出,所以防泄漏必须靠**结构纪律**(三条铁律)而不是测试。在用户的场景里意味着:任何需要长期运行的服务(如事件监听、后台轮询)都要把这三条写成 code-review 检查项,而不是指望压力测试发现。

## 疑点

"错误走同一 channel"在消费者需要并行处理多路来源时,结构体打包会让单通道成为瓶颈。原文未讨论该权衡——属未验证边界,不影响三铁律在单对 goroutine 场景的正确性。

## 术语

- **goroutine 泄漏**:goroutine 因 channel 无人接收而永久阻塞、无法退出,持续占用资源的现象。

## 连接

- → [[示例概念]] — 三铁律是"确定性优先"纪律在并发侧的表达;关联意义:任何想让我方多 agent 系统稳定运行的 harness,channel 纪律都是前置条件。
- → [[示例实体]] — 略,演示用。

## 引用
- 原文位置:[[examples/raw-demo.md]](合成演示,非真实源)
