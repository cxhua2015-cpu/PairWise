# metacatalog386

并发安全的内存型“元数据目录 386”(Go 1.22+,仅标准库)。实现见 `metacatalog386/servicecatalog.go`,语义以 `SPEC.md` 与契约测试为准。

## 设计要点

- **索引**:主索引为 `map[string]entry`,`entry` 保存 `value []byte` 与 `revision uint64`;另维护 `totalBytes`(当前 Value 总字节)、`generation`、`revision` 计数器,均可在 O(1) 内读取,无需遍历。
- **候选事务**:`Apply` 先对整个批次做完整结构校验(kind、名称字符集与长度、Value 长度、Delete 不携带 Value),不触碰任何状态;随后把当前 map 浅拷贝为候选 map,按输入顺序在其上执行 Put/Delete——Put 分配连续 revision,Delete 不分配且目标必须存在(`ErrNotFound`)。记录数与 Value 总字节容量只在批次末检查,越限返回 `ErrCapacity`。任一失败直接丢弃候选 map,已提交状态、generation、revision 全部不变(天然回滚);成功则整体换入候选 map,generation 恰好加一。空批次为无操作,不增加 generation。
- **所有权**:Put 的 Value 在写入候选前深拷贝;`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为新分配的副本,调用方对返回切片的修改不影响内部状态,反之亦然。`Snapshot.Records` 与 `Result.Changed` 均按名称排序;`Changed` 只包含批次结束后仍存在的被触及记录的最终版本。
- **并发**:单把 `sync.RWMutex`;`Apply` 持写锁,`Get`/`Snapshot` 持读锁,所有公开方法可安全并发调用。
- **复杂度**:结构校验 O(批次总字节);候选拷贝 O(n)(n 为当前记录数,仅拷贝指针级 entry);执行 O(批次总字节);`Changed` 构造 O(k log k)(k 为触及的不同名称数);`Get` O(1) 均摊加返回值拷贝;`Snapshot` O(n log n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
