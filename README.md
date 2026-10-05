# resourceledger087

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：账本主体是 `map[string]Account`（名称 → 账户），按名称 O(1) 定位。
`Top` 与 `Snapshot` 在读取时把 map 物化为切片并排序：Top 按数值降序、名称升序，
Snapshot 按名称升序。不为排序维护额外索引，以换取写入路径的简单与可预测。

**候选事务**：`Apply` 先做整批结构校验（kind 合法、名称非空且仅含
`[a-z0-9-_]`、字节长度不超限），不读取任何状态；随后在写锁内把当前账户表
浅拷贝为候选 map，按输入顺序在候选上执行 Add/Set/Delete。Add/Set 各分配一个
连续 revision；加法在算术前检测 int64 溢出，结果与 Set 值都执行绝对值上限
（`MaxAbsValue`）；Delete 缺失账户即 `ErrNotFound`。账户容量（`MaxAccounts`）
只在批次末对候选表检查。任一步失败直接返回，候选被丢弃，账本状态、
generation、revision 均不变，实现整体回滚；成功时才一次性提交候选，
非空批次 generation 恰好加一。

**所有权**：所有公开方法由一把 `sync.RWMutex` 保护（写操作独占，Top/Snapshot
共享读）。返回的 `[]Account` 与 `Result.Changed` 都是新分配的切片，账户为值类型，
调用方对返回值的任何修改都不会影响账本内部状态。

**复杂度**（n = 账户数，k = 批内 op 数）：
- `Apply`：校验 O(k·L)（L 为名称长度），执行 O(n + k)，候选拷贝 O(n)。
- `Top`：O(n log n) 排序后取前 m 个。
- `Snapshot`：O(n log n) 排序。
- 空间：O(n + k)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
