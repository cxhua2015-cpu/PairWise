# resourcecatalog181

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计

### 索引
- 主索引为 `map[string]Record`，以名称作为唯一键，Put/Delete/Get 均为 O(1) 均摊查找。
- 另维护 `totalValue`（Value 总字节）与 `nextRevision` 两个计数器，随批次原子更新，避免每次遍历时聚合。

### 候选事务（candidate transaction）
- `Apply` 先对所有 Op 做完整结构校验（kind、名称字符集与长度、Value 长度、Delete 不得携带 Value），任何失败直接返回 `ErrInvalidInput`，不触碰状态。
- 校验通过后，在写锁内克隆当前 map 得到候选状态，按输入顺序在候选上执行 Put/Delete；Put 分配连续 revision，Delete 不分配。
- 记录数与 Value 总字节容量只在批次末检查（`ErrCapacity`）；Delete 缺失记录返回 `ErrNotFound`。
- 任一步失败即丢弃候选，状态、generation、revision 完全不变；成功则整体换入候选，非空批次 generation 恰好 +1。

### 所有权
- 入库时深拷贝 Op 的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Record 均携带独立副本，调用方对返回切片的修改不影响内部状态，反之亦然。
- `Snapshot` 与 `Changed` 均按名称排序，返回切片与内部状态完全隔离。

### 并发
- 所有公开方法通过 `sync.RWMutex` 保护：`Apply` 持写锁，`Get`/`Snapshot` 持读锁，可多读者并发。

### 复杂度
- `Apply`：O(n + m)，n 为 Op 数，m 为当前记录数（候选克隆）；另加 O(k log k) 排序 Changed（k 为本批触及且仍存在的名称数）。
- `Get`：O(1) 均摊（外加 O(v) 拷贝，v 为 Value 长度）。
- `Snapshot`：O(m log m) 排序 + O(总字节) 深拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
