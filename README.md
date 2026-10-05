# resourceledger172

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：账本主体为 `map[string]Account`（名称 → 账户），O(1) 定位；`Top`/`Snapshot` 在读取时拷贝并排序，不维护持久有序结构，避免写路径的额外开销。
- **候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与字节上限），再在当前状态的拷贝（候选 map）上按输入顺序执行 Add/Set/Delete；算术前检测 int64 溢出并执行 `MaxAbsValue` 绝对值上限，`MaxAccounts` 容量仅在批次末检查。任一步失败直接丢弃候选，原状态零改动，实现整体回滚；成功时原子换入候选。
- **所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建拷贝，与内部状态完全隔离；调用方修改返回值不影响账本。`Account` 为纯值类型，map 换入后旧 map 不再被账本引用。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Top`/`Snapshot` 持读锁，可并发读取。
- **revision / generation**：Add/Set 按批次内顺序分配连续 revision（自 1 起）；非空成功批次 generation 恰好 +1，空批次与失败批次不变。
- **复杂度**：设批次长度 B、账户数 N。`Apply` 时间 O(N+B)（候选拷贝 + 顺序执行）、空间 O(N+B)；`Top(k)` 时间 O(N log N)、空间 O(N)；`Snapshot` 时间 O(N log N)、空间 O(N)；`New` O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
