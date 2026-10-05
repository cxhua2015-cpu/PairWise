# resourceledger127

并发安全的内存型资源计量账本。语义见 `SPEC.md`。

## 实现说明

- **索引**：账户状态存放在唯一的 `map[string]Account` 主索引中，按键 O(1) 定位。`Top` 与 `Snapshot` 在读取时即时排序（分别按值降序/名称升序、名称升序），不维护冗余有序结构，因此写入路径无额外索引维护成本。
- **候选事务**：`Apply` 先在无锁状态下对整批 Op 做纯结构校验（kind、名称字符集与长度），再在写锁内把主索引浅拷贝为候选 map，按输入顺序在其上执行 Add/Set/Delete；Add/Set 从单调递增的 `nextRevision` 分配连续 revision。int64 加法前先以符号检测溢出，随后执行绝对值上限；账户总数容量仅在批次末对候选结果检查。任一步失败直接丢弃候选，主状态、generation 与 revision 完全不变（整体回滚）；全部成功才一次性用候选替换主索引，非空批次 generation 恰好加一。
- **所有权**：`Ledger` 内部状态只通过 `sync.RWMutex` 保护的方法访问；`Apply`/`Top`/`Snapshot` 返回的 `Account` 切片均为新建拷贝，调用方修改返回值不会影响账本内部状态，多次调用互不共享内存。
- **复杂度**：设批次大小为 b、账户数为 n。`Apply` 结构校验 O(b)，候选拷贝 O(n)，执行 O(b)，合计 O(n+b) 时间、O(n+b) 额外空间；`Top(k)` 为 O(n log n) 时间、O(n) 空间；`Snapshot` 为 O(n log n) 时间、O(n) 空间。读操作（`Top`/`Snapshot`）使用读锁可并发执行，写操作（`Apply`）独占写锁。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
