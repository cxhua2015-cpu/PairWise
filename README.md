# balanceledger267

并发安全的内存型余额账本（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Add/Set/Delete，失败整体回滚；`Top` 按数值降序、名称升序，`Snapshot` 按名称排序。

## 多文件架构

- `creditpool.go` — 核心事务引擎：`New`/`Apply`/`Top`/`Snapshot` 与候选事务提交。
- `validation.go` — 无副作用的批次结构预检，`Apply` 与 `ValidateBatch` 共享同一套语义。
- `stats.go` — 线性一致的 `Stats` 汇总（同一把互斥锁下读取）。
- `clone.go` — 保留逻辑时钟（generation/nextRevision）的深拷贝。

## 索引

账户存储为 `map[string]Account`，按名称 O(1) 定位。`Top`/`Snapshot` 在读取时
物化并排序，不维护额外的有序索引，以换取写入路径 O(1) 与实现的简单性。

## 候选事务

`Apply` 先调用与 `ValidateBatch` 相同的结构校验（不读状态），再在互斥锁内把
当前账户表浅拷贝为候选 map，按输入顺序在其上执行全部操作：溢出在算术之前
用 `math.MaxInt64/MinInt64` 边界检测，绝对值上限逐操作执行，账户容量仅在
批次末检查。任一步失败直接丢弃候选，状态零副作用；成功则整体替换账户表，
非空批次 generation 恰好加一，revision 连续分配。

## 所有权

所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）都是新
分配的副本，`Account` 为纯值类型，调用方修改不影响内部状态。`Clone` 复制整个
账户 map 与逻辑时钟，克隆体与原账本完全独立、可各自并发演进。

## 并发与复杂度

单把 `sync.Mutex` 串行化所有公开方法，保证线性一致。设批次长度为 B、账户数为 N：

- `Apply`：O(N + B)（候选拷贝 + 顺序执行）
- `ValidateBatch`：O(B)，不读状态
- `Top`：O(N log N)；`Snapshot`：O(N log N)
- `Stats`：O(1)；`Clone`：O(N)

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
