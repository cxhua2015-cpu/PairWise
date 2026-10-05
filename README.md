# resourcelease104

并发安全的内存型“资源租约表”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，按键精确查找，Put/Touch/Delete 均为 O(1) 均摊。
- 未维护过期堆：每次 `Apply`/`Expire` 线性扫描并按闭区间 `ExpiresAt <= Now` 淘汰，单次淘汰 O(n)。表容量受 `MaxEntries` 约束，扫描成本有界。
- `Snapshot` 与 `Expire` 返回的条目按键字典序排序（规范顺序），排序 O(n log n)。

### 候选事务
- `Apply` 先对整个批次做结构校验（kind、键字符集与字节上限、非负时间），再做单调时间检查；任一失败不触碰状态。
- 校验通过后在候选副本（克隆的 map）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从 `nextRevision` 起分配递增 revision。
- 全部操作完成后做最终容量检查；容量超限或中途任何错误（如 Touch/Delete 未命中）直接丢弃候选，淘汰、时间与 revision 一并回滚，已观察状态不变。
- 非空成功批次 generation 恰好加一；空批次只推进时间与淘汰，不改变 generation。

### 所有权与并发
- 所有公开方法由单一 `sync.Mutex` 串行化，可安全并发调用。
- 返回的 `[]Entry`（`Expire`、`Snapshot`）均为新建切片与值拷贝，调用方修改不影响内部状态；`Entry` 为纯值类型，无共享指针。
- 时间为显式非负单调值：小于当前 `Now` 的 `Apply`/`Expire` 返回 `ErrTime` 且不回滚以外的任何变更。

### 复杂度
- `Apply`：O(n + m)，n 为现存条目数（克隆+淘汰），m 为批次操作数。
- `Expire`：O(n + k log k)，k 为过期条目数。
- `Snapshot`：O(n log n)（排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
