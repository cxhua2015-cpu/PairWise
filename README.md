# tokenvault

并发安全的内存型令牌到期库（Go 1.22+，仅标准库）。表使用显式非负单调时间，语义见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，键即令牌名，按Key O(1) 定位。
- 不维护额外的堆/有序索引：到期淘汰采用全表扫描（`ExpiresAt <= Now`，闭区间），在控制面规模下换取实现的简单与无额外写路径开销。
- `Snapshot` 与 `Expire` 返回的条目按 Key 排序，保证输出确定性。

## 候选事务

`Apply` 分两阶段执行：

1. **校验**：先对整个批次做完整结构校验（kind、键字符集与字节上限、非负时间），再检查时间单调性；任何一步失败都不触碰状态。
2. **候选提交**：在候选副本上先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete（Put/Touch 分配递增 revision），最后做容量检查。任何错误（NotFound、Capacity、Time）都直接丢弃候选，淘汰、时间与 revision 一并回滚；只有全部成功才原子提交，非空批次 generation 恰好加一。

## 所有权

- 所有公开方法（`Apply`/`Expire`/`Snapshot`）由单把互斥锁保护，可任意并发调用。
- 返回的切片（`Expire`、`Snapshot.Entries`）均为新建副本，调用方修改不影响内部状态；`Entry`/`Snapshot` 为纯值类型，无共享引用。

## 复杂度

设 n 为表中条目数、k 为批次操作数：

- `Apply`：校验 O(k)，候选复制 O(n)，执行 O(k)，合计 O(n + k) 时间、O(n) 额外空间。
- `Expire`：O(n) 扫描 + O(m log m) 排序（m 为到期条目数）。
- `Snapshot`：O(n log n)（复制并排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
