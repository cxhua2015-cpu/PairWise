# metacatalog331

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]entry`，`entry` 保存 `value []byte` 与 `revision uint64`，按名称 O(1) 查找。
- 另维护两个派生计数器：记录总数（`len(map)`）与 Value 总字节数 `totalValue`，随每次 Put/Delete 增量更新，批末容量检查为 O(1)。
- `generation` 与 `nextRevision` 为单调递增计数器；revision 从 1 开始连续分配。

### 候选事务（Apply）
1. **结构校验**：先对整个批次做纯结构校验（kind 合法、名称字符集/长度、Value 长度），不读取任何状态；失败返回 `ErrInvalidInput`。
2. **执行**：按输入顺序执行 Put/Delete。Put 分配连续 revision 并深拷贝 Value；Delete 不分配 revision，键不存在即失败。
3. **批末容量检查**：仅在整个批次执行完后检查记录数与 Value 总字节上限，超限返回 `ErrCapacity`。
4. **回滚**：执行期间对首次触碰的键保存旧值备份；任一步失败时恢复全部记录、`totalValue` 与 `nextRevision`，`generation` 不变。因此失败批次对外完全不可见。
5. 非空成功批次 `generation` 只加一；空批次成功且不改变任何状态。`Result.Changed` 按名称去重（同一键取最后一次操作的结果）并按名称排序。

### 所有权
- Put 时对 `Op.Value` 做深拷贝，调用方之后修改入参不影响目录。
- `Get`/`Snapshot` 返回的 `Record.Value` 与 `Records` 均为新分配的深拷贝，调用方修改返回值不影响内部状态，多次快照之间也互不影响。

### 并发
- 所有公开方法通过单一 `sync.Mutex` 串行化，批次整体原子，可与 `Get`/`Snapshot` 安全混发（`-race` 验证）。

### 复杂度
- `Apply`：O(n) 结构校验 + O(n) 执行 + O(c log c) 对 `Changed` 排序（n 为批内操作数，c 为涉及的不同键数）。
- `Get`：O(1) 查找 + O(v) 拷贝（v 为 Value 长度）。
- `Snapshot`：O(r log r) 排序 + O(V) 拷贝（r 为记录数，V 为 Value 总字节数）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
