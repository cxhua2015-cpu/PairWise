# credentiallease

并发安全的内存型凭据租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引结构

- 主索引为 `map[string]Entry`，键到租约条目 O(1) 定位；不维护额外的堆或有序索引。
- 过期淘汰采用全量扫描（`ExpiresAt <= Now`，闭区间），单次 Apply/Expire 为 O(n)。
- `Snapshot` 与 `Expire` 返回的条目按键字典序排序，保证规范、可比较的输出顺序。

## 候选事务

`Apply` 分两阶段：

1. **校验**：先对整个批次做结构校验（kind、键字符集与字节上限、非负时间），再在锁内检查时间单调性（`Now < now` 返回 `ErrTime`）。
2. **候选提交**：在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一次递增 revision。任何错误（`ErrNotFound`、最终容量 `ErrCapacity`）都会整体回滚——淘汰、时间与 revision 均不落盘。非空成功批次 generation 恰好加一，空批次不变。

## 所有权与并发

- 所有公开方法由单一 `sync.Mutex` 保护，可并发调用。
- `Snapshot`/`Expire` 返回的切片是新分配的副本，调用方修改不影响内部状态；`Entry` 为纯值类型。
- 时间由调用方显式提供，表内不读取墙钟。

## 复杂度

- `New`：O(1)；`Apply`：O(n + m)，n 为条目数、m 为批次操作数；`Expire`：O(n)；`Snapshot`：O(n log n)（排序）。
- 空间：O(n)，受 `Options.MaxEntries` 上限约束。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
