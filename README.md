# budgetledger

并发安全的内存型预算账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：账本主体是 `map[string]Account` 哈希索引，按名称 O(1) 定位账户。不维护有序索引——`Top` 与 `Snapshot` 在读取时按需对当前账户集合排序（值降序/名称升序，或纯名称升序），因为账户数受 `MaxAccounts` 上限约束，排序代价有界，换来写入路径的常数时间。

**候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与长度、多余字段），不触碰状态；随后在 `accounts` 的浅拷贝（候选 map）上按输入顺序执行 Add/Set/Delete，溢出与绝对值上限在每次算术前检测，账户容量仅在批次末检查。任何一步失败直接丢弃候选 map，实现零成本整体回滚；全部通过才用候选 map 原子替换内部状态，并将 generation 加一、推进 revision 计数器。

**所有权**：所有公开方法由一把 `sync.RWMutex` 保护——`Apply` 持写锁，`Top`/`Snapshot` 持读锁可并发。返回的 `[]Account`（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的切片，`Account` 为纯值类型，调用方修改返回值不会影响内部状态；`New` 之后 `Options` 各上限被拷贝进 `Ledger`，与调用方解耦。

**复杂度**（n = 批内 op 数，m = 当前账户数，k = Top 请求数）：
- `Apply`：O(n) 校验 + O(m) 候选拷贝 + O(n) 执行，空间 O(m)。
- `Top`：O(m log m) 排序 + O(k) 拷贝。
- `Snapshot`：O(m log m) 按名排序。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
