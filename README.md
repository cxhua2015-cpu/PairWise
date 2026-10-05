# resourcecatalog171

并发安全的内存型资源目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete，Put 分配连续 revision，Delete 不分配；失败整体回滚。

## 设计

- **索引**：`Store` 内以 `map[string]Record` 为主索引（按名称 O(1) 定位），
  另维护 `totalValue`、`generation`、`nextRevision` 计数器，避免全表扫描。
- **候选事务**：`Apply` 先在克隆的 map 上按序执行整个批次（候选状态），
  批次末统一校验记录数与 Value 总字节容量；任何失败直接丢弃候选状态，
  已提交的 records/generation/revision 均不受影响，实现天然回滚。
- **所有权**：Put 时深拷贝 `Value` 入库；`Get`/`Snapshot` 返回深拷贝，
  调用方对返回切片的修改不影响内部状态，反之亦然。
- **并发**：单把 `sync.Mutex` 保护全部内部状态，所有公开方法并发安全；
  结构校验、候选执行、提交均在临界区内完成，批次之间串行化。
- **复杂度**：结构校验 O(批次大小)；候选执行 O(批次大小 + 克隆记录数)；
  `Get` O(1)；`Snapshot` O(n log n)（按名称排序）。空批次不增加 generation，
  非空成功批次 generation 恰好 +1。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
