# resourcelease114

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，键即租约名，查找 / 插入 / 删除均为 O(1)。
- `Snapshot` 与 `Expire` 返回的条目按键字典序排序，保证输出确定性；返回切片均为新建拷贝，与内部状态完全隔离，调用方可自由修改。

### 候选事务（Apply）
1. **结构校验**：先校验整个批次（kind 合法、键非空且仅含 `[a-z0-9-_]`、不超过 `MaxKeyBytes`、Put/Touch 的 `ExpiresAt` 非负），任何错误返回 `ErrInvalidInput`，此阶段不读取状态。
2. **时间检查**：`Now < 当前时间` 返回 `ErrTime`；时间只前进不后退。
3. **候选状态**：复制当前索引，先在候选上淘汰 `ExpiresAt <= Now`（闭区间）的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个单调递增的 revision。
4. **提交或回滚**：最终条目数超过 `MaxEntries` 返回 `ErrCapacity`；任何错误（含 `ErrNotFound`）都会连同淘汰、时间与 revision 一起回滚，内部状态保持不变。仅当批次成功且非空时 generation 恰好加一；空批次不改变 generation。

### 所有权
- `Table` 通过 `New` 返回，调用方独占指针；所有公开方法由互斥锁保护，可并发调用。
- 传入的 `Batch`/`Op` 按值读取，不保留引用；返回的 `[]Entry` 与 `Snapshot` 为深拷贝，内部 map 永不外泄。

### 复杂度
- Put/Touch/Delete 单操作：O(1)。
- `Apply`：O(n + m)，n 为批次数、m 为现存条目数（候选复制与淘汰）。
- `Expire`：O(m + k log k)，k 为到期条目数（排序）。
- `Snapshot`：O(m log m)（排序输出）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
