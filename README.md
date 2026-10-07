# expirytable349

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引与数据结构

- 内部使用 `map[string]Entry` 作为唯一主索引，键到条目 O(1) 定位。
- 不维护额外的按时间排序索引：到期扫描（Apply 候选淘汰与 `Expire`）为全表 O(n) 遍历，换取实现简单与回滚容易。
- `Snapshot` 与 `Expire` 返回的切片按键排序，保证输出确定性。

## 候选事务（Apply）

1. 先对整个批次做结构校验（kind、键字符集/长度、非负 ExpiresAt），再检查时间单调性。
2. 在候选状态（条目映射的副本）上先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
3. 任何错误（`ErrNotFound`、最终 `ErrCapacity` 等）都会整体回滚：淘汰、时间、revision、generation 均不变。
4. 非空成功批次 generation 只增加一次；空批次不改变 generation，仅推进时间。

## 所有权与并发

- 所有公开方法由单个 `sync.Mutex` 保护，可安全并发调用。
- `Snapshot`/`Expire` 返回的切片与内部状态完全隔离，调用方可自由修改。
- 表不共享调用方传入的切片；键为字符串值语义，无别名风险。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰扫描），m 为批次内操作数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序）。
- `Snapshot`：O(n log n)（排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
