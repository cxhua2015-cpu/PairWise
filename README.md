# imagecatalog

并发安全的内存型“镜像目录”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]entry`，`entry` 保存深拷贝后的 `Value` 与单调递增的 `Revision`。
- 不维护额外有序结构；`Snapshot`/`Result.Changed` 在读取时按名称排序，写入路径保持 O(1)。

### 候选事务（两阶段提交）
`Apply` 在单把 `sync.Mutex` 下分三个阶段执行：
1. **结构校验**：对全部 Op 校验 kind、名称字符集与长度、Value 长度、Delete 不得携带 Value；任何失败立即返回 `ErrInvalidInput`，不读取任何状态。
2. **候选执行**：把当前索引浅拷贝（`entry` 为不可变值，拷贝即隔离）为候选 map，按输入顺序应用 Put/Delete；Put 递增候选 revision，Delete 未命中返回 `ErrNotFound`。
3. **容量检查与提交**：仅在批次末检查记录数与 Value 总字节，超限返回 `ErrCapacity`；通过后用候选 map 整体替换索引并递增一次 generation。任何失败路径都不触碰共享状态，因此回滚是“零成本”的——候选直接丢弃。

### 所有权
- 写入时拷贝调用方的 `Value`；`Get`/`Snapshot`/`Result.Changed` 返回的切片均为新分配的深拷贝，调用方与 Store 互不别名。
- 空批次为无操作：不递增 generation，返回当前计数器。

### 并发
所有公开方法共用一把互斥锁。读操作（`Get`/`Snapshot`）与写操作（`Apply`）互斥，快照天然一致，无撕裂读。

### 复杂度
- `Apply`：O(B + N)，B 为批内 Op 数，N 为当前记录数（候选拷贝与容量求和）。
- `Get`：O(1) 均摊（外加一次 Value 拷贝）。
- `Snapshot`：O(N log N)（排序）+ O(总字节数)（深拷贝）。
- 空间：O(N + 总字节数)。

## 使用

```go
s, _ := imagecatalog.New(imagecatalog.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
res, _ := s.Apply(imagecatalog.Batch{Ops: []imagecatalog.Op{{Kind: imagecatalog.Put, Name: "alpha", Value: []byte("v")}}})
snap := s.Snapshot()
```

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
