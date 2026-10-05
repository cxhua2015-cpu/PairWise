# resourcelease179

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引与所有权

- 主索引为 `map[string]Entry`，键到租约条目 O(1) 定位；无堆/二级索引，淘汰采用全表扫描。
- 表内条目归 `Table` 独占所有；`Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方修改不影响内部状态。
- 所有公开方法经 `sync.RWMutex` 保护：`Apply`/`Expire` 持写锁，`Snapshot` 持读锁，可任意并发调用。

## 候选事务（Apply）

1. 先对整个批次做结构校验（kind、键字符集与字节上限、非负 ExpiresAt），再检查时间（非负且单调不回退）。
2. 克隆当前条目表作为候选状态：先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
3. 最终容量超限或任一操作失败（如 Touch/Delete 缺失键）时整体回滚——淘汰、时间、revision、generation 均不变；成功时一次性提交，非空批次 generation 恰好加一，空批次不改变任何状态。

`Expire(now)` 使用相同的闭区间边界与单调时间检查，返回被淘汰条目（按键排序）。

## 复杂度

- `Apply`：O(N + M)，N 为现存条目数（克隆与淘汰扫描），M 为批次操作数。
- `Expire`：O(N + K log K)，K 为被淘汰条目数（排序）。
- `Snapshot`：O(N log N)（复制并按键排序，保证确定性输出）。
- 空间：O(N)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
