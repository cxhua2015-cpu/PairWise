# tokenbucket

并发安全、使用显式时间的内存多租户令牌桶注册表（Go 1.22+，仅标准库）。公开契约见 `SPEC.md`。

## 索引结构

`Registry` 内部使用 `map[string]*bucket` 按桶名索引，并维护一份按名字排序的桶名切片，用于 `Snapshot` 的稳定排序输出。所有公开方法通过一把 `sync.Mutex` 串行化，保证并发安全；`Inspect`/`Snapshot` 返回的是拷贝出的值数据，与内部状态完全脱离。

## 补充算法

每个桶独立按完整周期离散补充：在显式时间 `at`，获得 `floor((at-LastRefill)/RefillEvery) * RefillTokens` 个令牌并封顶到 `Capacity`。`LastRefill` 总是推进到最近一个完整补充边界（即使桶已满），`LastObserved` 推进到 `at`。

## 事务语义

`ApplyBatch` 分两阶段：

1. 对批次中**全部**变更先做结构校验（Kind 合法、桶名结构合法、Tokens 为正、At 非负），失败返回 `ErrInvalidInput`，不触碰任何状态。
2. 在克隆出的隔离候选状态上按输入顺序执行：未知桶 `ErrNotFound`、`At < LastObserved` 为 `ErrTimeBackwards`；每条变更先按 `At` 补充，Acquire 余额不足返回 `ErrInsufficient`，Refund 超过容量返回 `ErrOverflow`。任何失败整体回滚（候选直接丢弃）；全部成功才一次性提交并将 generation 加一（空批次只读返回当前 generation）。

## 时间模型

时间是调用方显式传入的 int64，不使用墙钟。每个桶的 `LastObserved` 单调不减；`Sweep(at)` 要求 `at` 不小于所有桶的 `LastObserved`，统一推进所有桶，且仅当至少一个桶的 Tokens/LastRefill/LastObserved 实际变化时才将 generation 加一。`Inspect` 计算未来状态但不产生副作用。`NextRefill` 在桶已满或下一边界超出 int64 域时为 0，否则为 `LastRefill + RefillEvery`。

## 溢出处理

所有 int64 运算避免溢出：补充前先判断饱和（比较所需周期数与已流逝周期数）再做乘法；新补充边界用 `at - elapsed%RefillEvery` 计算而非累加；Refund 用 `Tokens > Capacity - balance` 判定；`NextRefill` 在加法前检查 `RefillEvery <= MaxInt64 - LastRefill`。`math.MaxInt64` 级别的容量、补充量与周期均可安全工作。

## 复杂度

设 B 为桶数、N 为批次大小：

- `New`：时间 O(B log B)（排序桶名），空间 O(B)。
- `ApplyBatch`：时间 O(B + N)（克隆全部桶状态），空间 O(B)。
- `Inspect`：时间 O(1)，空间 O(1)。
- `Sweep`：时间 O(B)，空间 O(1)。
- `Snapshot`：时间 O(B)，空间 O(B)。
- 总常驻空间 O(B)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
