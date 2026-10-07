# balanceledger407

并发安全的内存型余额账本，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 架构

实现按职责拆分为四个联动文件：

- `creditpool.go` — 核心事务引擎：`New` / `Apply` / `Top` / `Snapshot`。
- `validation.go` — 无副作用的批次结构预检：`ValidateBatch`，与 `Apply` 共享同一套 `validateOp` / `validateName` 结构语义。
- `stats.go` — 线性一致的状态摘要：`Stats`（读锁下读取，与并发事务一致）。
- `clone.go` — 保留逻辑时钟（generation / nextRevision）的深拷贝：`Clone`。

## 索引

账户主索引为 `map[string]Account`（按名称 O(1) 定位）。`Top` 与 `Snapshot`
在读取时物化并排序，不维护额外的有序结构，以换取写入路径的极简与无锁竞争。

## 候选事务

`Apply` 先调用 `ValidateBatch` 做完整结构校验（不读状态），再在写锁内把当前
索引浅拷贝为候选 map，按输入顺序在其上执行 Add/Set/Delete：Add/Set 分配连续
revision，Add 在做任何算术前检测 int64 溢出并执行绝对值上限；最终账户容量
仅在批次末检查。任一步失败直接丢弃候选 map，实现整体回滚；全部成功才一次性
换入并推进 generation（非空成功批次 +1，空批次不变）。

## 所有权

所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新
分配的副本，与内部状态完全隔离；`Clone` 逐条复制账户，新旧账本互不影响。
并发安全由单个 `sync.RWMutex` 保证：写事务持写锁，`Top` / `Snapshot` /
`Stats` / `Clone` 持读锁。

## 复杂度

- `Apply`：O(n + a)，n 为批内 op 数，a 为当前账户数（候选拷贝）。
- `ValidateBatch`：O(n)，不触碰状态。
- `Top`：O(a log a)；`Snapshot`：O(a log a)；`Stats`：O(1)；`Clone`：O(a)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
