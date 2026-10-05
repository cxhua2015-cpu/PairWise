# capacityledger

并发安全的内存型容量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Ledger` 以 `map[string]Account` 作为主索引，按账户名 O(1) 定位。
`Top` 与 `Snapshot` 不维护辅助有序结构，而是在读路径上对当前账户集合做一次性
排序（分别为值降序/名称升序、名称升序），以换取写路径 O(1) 与实现的简洁性。

**候选事务（candidate transaction）**：`Apply` 先在持锁状态下做完整结构校验，
随后把被触及账户的当前值复制到 `staged` 暂存映射中，按输入顺序在暂存区执行
Add/Set/Delete；算术前先检测 int64 溢出并执行绝对值上限，账户数容量上限仅在
批次末检查。任一失败直接返回，主索引未被触碰，天然实现整体回滚；全部成功才
一次性提交并推进 `generation`/`revision`。

**所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）
均为新建并拷贝的切片，调用方修改不会影响内部状态；内部 `Account` 为值类型，
不存在共享指针。`Ledger` 内部状态绝不逃逸。

**并发**：单把 `sync.RWMutex` 保护全部状态。`Apply` 走写锁，`Top`/`Snapshot`
走读锁，空批次只读。所有公开方法可安全并发调用（含 `-race` 验证）。

**复杂度**（n = 批次数，m = 账户总数）：
- `Apply`：O(n) 时间，O(n) 额外空间（暂存区）。
- `Top(k)`：O(m log m) 排序 + O(k) 拷贝。
- `Snapshot`：O(m log m) 排序 + O(m) 拷贝。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
