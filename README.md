# rewardledger

并发安全的内存型奖励积分账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引与所有权**
- 主索引为 `map[string]Account`，按账户名直接定位；`Ledger` 独占该 map，账户记录按值存储。
- 一把 `sync.RWMutex` 保护全部状态：`Apply` 持写锁，`Top`/`Snapshot` 持读锁，所有公开方法可并发调用。
- `Top`/`Snapshot` 在锁内复制记录到独立切片后再排序返回，调用方修改返回切片不影响内部状态（所有权随返回值转移）。

**候选事务（candidate transaction）**
- `Apply` 先做整批结构校验（kind、名称字符集与字节上限），不读取任何状态。
- 随后在写锁内把当前 map 克隆为候选状态，按输入顺序在候选上执行 Add/Set/Delete：
  - Add 在算术前检测 int64 溢出（含 `math.MinInt64` 边界），结果执行 `±MaxAbsValue` 绝对值上限；
  - Set 直接对目标值执行上限检查；Delete 要求账户存在，否则 `ErrNotFound`；
  - Add/Set 各自分配连续递增的 revision。
- 账户容量 `MaxAccounts` 仅在批次末对候选状态检查（允许批中先超后降）。
- 任一步失败直接丢弃候选，已提交状态零改动（整体回滚）；成功则原子替换 map，
  非空批次 generation 恰好加一，空批次不改变任何计数器。

**复杂度**（n = 账户数，b = 批次内 op 数）
- `Apply`：时间 O(n + b)（克隆候选 + 逐 op O(1)），失败时额外空间 O(n)。
- `Top(k)`：时间 O(n log n)，空间 O(n)。
- `Snapshot`：时间 O(n log n)（按名称排序），空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
