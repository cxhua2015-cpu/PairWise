# tokenbucket

并发安全、使用显式时间的内存多租户令牌桶注册表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引结构

`Registry` 内部为 `sync.Mutex` 保护的 `map[string]*bucket`，按桶名 O(1) 定位。所有公开方法在单个互斥锁下执行，因此并发调用彼此线性化；`Snapshot` 按桶名排序后返回拷贝，`Inspect`/`Snapshot` 返回的都是脱离注册表的值数据。

## 补充算法

离散补充：在显式时间 `at`，桶获得 `floor((at-LastRefill)/RefillEvery) * RefillTokens` 个令牌并封顶到 `Capacity`。`LastRefill` 通过 `at - elapsed%RefillEvery` 推进到最近的完整周期边界（即使桶已满也推进），`LastObserved` 推进到 `at`。`NextRefill` 在桶已满或 `LastRefill+RefillEvery` 超出 int64 域时为 0，否则为下一个边界。

## 事务

`ApplyBatch` 先对全部变更做结构校验（Kind、名称字符集与长度、Tokens>0、At>=0，返回 `ErrInvalidInput`），再克隆全部桶状态为隔离候选，按输入顺序执行：未知桶 `ErrNotFound`、`At<LastObserved` 为 `ErrTimeBackwards`、余额不足 `ErrInsufficient`、返还超容量 `ErrOverflow`。任何失败直接丢弃候选，整体回滚；成功则一次性换入候选并将 generation 加一（空批次与失败批次不加）。

## 时间

时间完全由调用方显式提供，注册表不读取系统时钟。每个桶维护单调的 `LastObserved`，`Sweep(at)` 要求对所有桶都不倒退，且仅当至少一个桶的 Tokens/LastRefill/LastObserved 实际变化时才将 generation 加一。

## 溢出处理

所有 int64 运算先做饱和判定再计算：补充时先比较 `periods > (Capacity-Tokens)/RefillTokens` 再决定封顶或累加，避免乘法溢出；返还时比较 `Tokens > Capacity-Tokens`；`NextRefill` 在加法会溢出时归 0。因此 `math.MaxInt64` 级别的容量、周期与时间均安全。

## 复杂度

设 B 为桶数、N 为批次变更数：

- `New`：O(B) 时间，O(B) 空间。
- `ApplyBatch`：O(B+N) 时间（克隆候选 O(B)），O(B) 额外空间。
- `Inspect`：O(1) 时间，O(1) 额外空间。
- `Sweep`：O(B) 时间，O(1) 额外空间。
- `Snapshot`：O(B log B) 时间（排序），O(B) 空间。

注册表总空间为 O(B)。
