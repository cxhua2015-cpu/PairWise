# resourcelease184

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

- **索引**：单把 `sync.Mutex` 保护全部状态；条目存于 `map[string]Entry`（键 → 租约），按键 O(1) 定位。`Snapshot`/`Expire` 返回的切片按键排序，保证确定性输出。
- **候选事务**：`Apply` 先在锁外做完整结构校验（kind、键字符集与长度、非负时间），再持锁检查单调时间；随后在候选 map 上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete 并分配 revision，最后做容量检查。任一步失败直接丢弃候选，淘汰、时间与 revision 随之一并回滚，已提交状态零副作用。
- **所有权**：所有公开方法返回的切片/结构均为新建副本，与内部状态隔离；调用方修改返回值不影响表。空批次成功时不增加 generation，非空成功批次 generation 只加一。
- **复杂度**：`Apply` 为 O(n + m)（n 为现存条目数，m 为批内操作数；候选复制 O(n)，淘汰扫描 O(n)，操作 O(m)）；`Expire` O(n + k log k)（k 为过期数，排序）；`Snapshot` O(n log n)；`New` O(1)。所有方法在互斥锁下并发安全。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
