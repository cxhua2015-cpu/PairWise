# reservationlease

并发安全的内存型“预留租约表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，键即租约名，Put/Touch/Delete 均为 O(1) 均摊。
- 未维护按过期时间排序的辅助索引；淘汰与 `Expire` 采用全表扫描 O(n)，换取实现简单与无额外写路径开销。
- `Snapshot` 与 `Expire` 返回的条目按字典序排序，保证输出确定性。

## 候选事务

`Apply` 采用候选状态（copy-on-write）事务模型：

1. 先对整个批次做结构校验（kind、键字符集与长度、`ExpiresAt >= 0`），再检查时间单调性。
2. 在候选副本上先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
3. 最终容量超限或任何错误（`ErrNotFound`、`ErrCapacity` 等）都会整体回滚——淘汰、时间、revision、generation 均不生效；成功时才一次性提交。
4. 非空成功批次 generation 只加一；空批次不改变 generation。

## 所有权

- 表内部状态不对外暴露：`Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不会影响表。
- 传入的 `Batch`/`Op` 仅按值读取，实现不保留其引用。

## 并发与复杂度

- 所有公开方法由单一 `sync.Mutex` 保护，可安全并发调用；批次整体串行化，保证单调时间与计数器一致。
- `Apply`：结构校验 O(L)（L 为批次键总字节数）+ 候选复制 O(n) + 逐 op O(1)。
- `Expire`：O(n) 扫描 + O(k log k) 排序（k 为过期条目数）。
- `Snapshot`：O(n log n) 排序复制。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
