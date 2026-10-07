# metacatalog366

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**：`Store` 内部使用 `map[string]entry` 作为主索引，键为记录名，`entry` 持有
深拷贝后的 Value 与分配到的 revision。配合同一个 `sync.Mutex` 保护的
`generation` / `revision` 计数器，所有公开方法（`Apply`/`Get`/`Snapshot`）
均串行化执行，天然并发安全。

**候选事务**：`Apply` 先在持锁状态下对整个批次做完整结构校验（kind、名称字符集
与长度、Value 长度、Delete 不携带 Value），任何结构错误在读取状态前返回
`ErrInvalidInput`。随后在现有 map 的克隆（候选事务）上按输入顺序执行
Put/Delete：Put 分配连续 revision，Delete 不分配；Delete 缺失键返回
`ErrNotFound`。记录数与 Value 总字节容量只在批次末检查，超限返回
`ErrCapacity`。任一步失败直接丢弃候选 map，原始状态、generation 与 revision
完全不变；全部成功才用候选 map 替换内部状态，generation 只增加一次（空批次不变）。
`Result.Changed` 按名称去重（保留最后一次效果）并按名称排序返回。

**所有权**：写入时 Put 的 Value 会被深拷贝后存入；读取时 `Get`、`Snapshot`、
`Result.Changed` 中的 Value 均为新分配的副本。调用方对传入或返回切片的任何
修改都不会影响内部状态，反之亦然。

**复杂度**（n = 当前记录数，m = 批次内 op 数，B = 涉及 Value 总字节数）：
- `Apply`：校验 O(m)；候选克隆 O(n)；执行 O(m + B)；容量检查 O(n)；
  Changed 排序 O(m log m)。空间 O(n + B)。
- `Get`：O(1) 查询 + O(|value|) 拷贝。
- `Snapshot`：O(n log n) 排序 + O(总字节数) 深拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
