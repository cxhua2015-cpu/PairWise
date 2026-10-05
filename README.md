# resourcecatalog161

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

**索引**：`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护 `total`（Value 总字节数）、`gen`（generation）与 `nextRev`（下一个待分配 revision）三个标量。名称到记录没有二级索引，Snapshot/Changed 的排序在读取时按需进行。

**候选事务**：`Apply` 先做完整结构校验（kind、名字符集与长度、Value 长度），不触碰任何状态；随后在加写锁后把当前 map 浅拷贝为候选副本，按输入顺序在副本上执行 Put/Delete——Put 分配连续 revision，Delete 要求记录存在。仅在全部操作成功后，于批次末检查记录数与 Value 总字节容量；任一失败直接丢弃候选副本，状态、generation、revision 全部天然回滚。非空成功批次 generation 只加一，空批次不变。

**所有权**：Put 的 Value 在入库时深拷贝；`Get`/`Snapshot` 返回的 Record 同样深拷贝 Value，调用方对返回切片的任何修改都不会影响内部状态，反之亦然。`Snapshot` 与 `Result.Changed` 均按名称排序。

**并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Get`/`Snapshot` 持读锁，可并行读取。

## 复杂度

- `Apply`：O(n + m log m)，n 为批次数与记录拷贝，m 为变更名数（排序）。
- `Get`：O(1) 定位 + O(v) 拷贝，v 为 Value 长度。
- `Snapshot`：O(r log r)，r 为记录数（排序 + 深拷贝）。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
