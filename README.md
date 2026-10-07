# balanceledger372

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：账户状态存放在唯一的 `map[string]Account` 主索引中，按名称 O(1) 定位。`Top` 与 `Snapshot` 在读取时把主索引物化为切片并排序，不维护冗余的有序索引，从而避免写路径上的额外一致性问题。

**候选事务**：`Apply` 先在持锁状态下完整校验批次结构（kind、名称合法性），再把主索引克隆为候选 map，在候选上按输入顺序执行 Add/Set/Delete 并分配连续 revision，最后在批次末检查账户容量。任一步失败直接丢弃候选，账本保持原状，实现整体回滚；全部成功才用候选替换主索引并将 generation 递增一次。

**所有权**：`Result.Changed`、`Top`、`Snapshot` 返回的切片及其中的 `Account` 值均为新建副本，调用方修改返回值不会影响账本内部状态；内部 map 也只在持锁时替换，绝不共享给调用方。

**并发**：单把 `sync.RWMutex` 保护全部状态。`Apply` 取写锁，`Top`/`Snapshot` 取读锁，可并发执行。

**复杂度**：设批次长度为 B、账户数为 N。`Apply` 为 O(N + B)（克隆候选 map 加顺序执行）；`Top` 为 O(N log N)；`Snapshot` 为 O(N log N)（按名称排序）；`New` 为 O(1)。溢出在算术前用边界比较检测，绝对值上限在每次 Add/Set 后立即执行，账户容量仅在批次末检查。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
