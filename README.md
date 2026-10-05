# resourceledger117

并发安全的内存型资源计量账本。原子批次按输入顺序执行 `Add`/`Set`/`Delete`，
`Add`/`Set` 分配连续 revision，算术前检测 int64 溢出并执行绝对值上限，
账户容量仅在批次末检查，失败整体回滚。仅依赖标准库（Go 1.22+）。

## 设计说明

- **索引**：账户存于 `map[string]Account` 哈希索引，查找/写入 O(1)。
  `Top` 与 `Snapshot` 在读取时物化切片并排序，不维护持久有序结构，
  以换取写路径的常数时间。
- **候选事务**：`Apply` 先对整个批次做纯结构校验（kind、名称合法性），
  不读取状态；随后在互斥锁内对账户映射的**克隆副本**逐项执行操作，
  任一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃副本，
  成功才整体替换内部映射并推进 generation / nextRevision，实现原子回滚。
- **所有权**：`Result.Changed`、`Top`、`Snapshot` 返回的切片均为每次调用
  新建的副本，调用方修改不会影响内部状态；内部状态只在锁内被替换，
  绝不就地复用已暴露的切片。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 取写锁，
  `Top`/`Snapshot` 取读锁，可并发执行。

## 复杂度

设批次长度为 B、账户数为 N：

- `Apply`：结构校验 O(B)；克隆 O(N)；执行 O(B)；提交 O(1) 替换。
- `Top(k)`：物化 O(N) + 排序 O(N log N)。
- `Snapshot`：物化 O(N) + 按名称排序 O(N log N)。
- 空间：O(N)，候选事务期间临时 O(N)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
