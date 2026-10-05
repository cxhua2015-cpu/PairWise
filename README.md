# resourcecatalog186

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]record` 作为主索引，键为资源名称，值持有深拷贝后的
`Value` 与分配的 `Revision`；另维护 `generation`、`revision` 计数器与 `totalBytes`
（当前所有 Value 字节总数），避免每次容量检查都全表扫描。

**候选事务**：`Apply` 先在无锁状态下对整个批次做完整结构校验（kind、名称字符集与长度、
Value 长度），再在写锁内把当前 map 克隆为候选副本，按输入顺序在副本上执行 Put/Delete；
Put 递增并分配连续 revision，Delete 不分配。记录数与 Value 总字节容量只在批次末检查。
任何失败直接丢弃候选副本——状态、generation、revision 天然全部回滚；成功则整体提交，
非空批次 generation 只加一，空批次不变。

**所有权**：Put 的 Value 在入库前拷贝；`Get`、`Snapshot`、`Result.Changed` 返回的 Value
均为新分配的深拷贝，返回切片与内部状态完全隔离，调用方修改互不影响。

**并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Get`/`Snapshot` 持读锁，
所有公开方法可安全并发调用，批次之间可串行化。

**复杂度**：设批次含 b 个 op、存储 n 条记录、平均 Value 长度 v。`Apply` 为
O(b·v + n)（候选克隆 + 逐 op 应用），`Get` 为 O(v)，`Snapshot` 为 O(n·v + n log n)
（深拷贝 + 按名称排序）；空间 O(n·v)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
