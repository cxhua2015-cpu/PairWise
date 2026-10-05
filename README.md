# resourcelease154

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

### 索引
- 主索引为 `map[string]Entry`，键即租约键，Put/Touch/Delete/Expire 均为 O(1) 均摊定位。
- 不维护额外的过期堆/有序索引：淘汰在 Apply/Expire 时以一次全表扫描完成（O(n)，n 为当前条目数），实现简单且无额外写路径开销。
- `Snapshot` 与 `Expire` 返回的条目按键排序，保证输出确定性。

### 候选事务
- `Apply` 分两阶段：先做整批结构校验（kind、键字符集与长度、非负时间），再在锁内检查单调时钟。
- 通过后复制一份候选 map，在候选上先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete（Put/Touch 分配递增 revision），最后做容量检查。
- 任一步失败（ErrNotFound / ErrCapacity / ErrTime 等）直接丢弃候选，淘汰、时间和 revision 随之一并回滚，已提交状态不受影响。
- 成功时整体提交候选，`Now` 前进，非空批次 generation 恰好加一；空批次不改变任何状态。

### 所有权与并发
- 所有公开方法由单把 `sync.Mutex` 保护，可安全并发调用；批次内操作串行化，批次间线性一致。
- `Snapshot.Entries` 与 `Expire` 返回值均为新分配的切片与条目副本，调用方修改不会影响内部状态。
- 错误值为包级 `var`，可用 `errors.Is` 判定。

### 复杂度
- `Apply`：O(n + m)，n 为现有条目数（候选复制 + 淘汰扫描），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为过期条目数（排序）。
- `Snapshot`：O(n log n)（复制 + 排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
