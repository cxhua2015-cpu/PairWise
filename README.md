# heartbeat

并发安全的内存型节点心跳表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，键即节点名，Put/Touch/Delete/过期扫描均为 O(n) 以内的单次遍历或 O(1) 定位。
- `Snapshot` 与 `Expire` 返回的条目按键字典序排序（规范顺序），保证输出确定性。

### 候选事务（Apply）
1. 先对整个批次做结构校验（Now 非负、Kind 合法、键字符集与长度上限），再检查时间单调性（`Now >= 当前 now`，否则 `ErrTime`）。
2. 在候选状态（条目表的浅拷贝）上先删除 `ExpiresAt <= Now` 的条目（闭区间），再按顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
3. 最后做容量检查：候选条目数超过 `MaxEntries` 返回 `ErrCapacity`。
4. 任何错误都直接丢弃候选状态——淘汰、时间、revision、generation 全部回滚；只有全部成功才一次性提交，非空成功批次 generation 恰好加一，空批次不改变任何状态。

### 所有权与并发
- 所有公开方法（`Apply`/`Expire`/`Snapshot`）由同一把互斥锁保护，可任意并发调用。
- 返回的切片（`Snapshot.Entries`、`Expire` 结果）均为新分配的副本，调用方修改不会影响表内状态，反之亦然。
- `Entry`/`Snapshot` 等均为值语义，表内不保留调用方传入的切片。

### 复杂度
- `Apply`：O(n + m)，n 为当前条目数（克隆 + 过期扫描），m 为批次内操作数。
- `Expire`：O(n + k log k)，k 为被淘汰的条目数（排序）。
- `Snapshot`：O(n log n)（排序输出）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
