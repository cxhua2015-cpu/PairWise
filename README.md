# expirytable384

并发安全的内存型“到期状态表 384”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，键到条目 O(1) 定位。
- `Snapshot` 与 `Expire` 返回的条目按字典序排序，保证输出确定性；返回切片均为新建副本，与内部状态完全隔离（所有权移交调用方，修改不影响表）。

### 候选事务
- `Apply` 采用候选事务（copy-on-write）：先对整个批次做结构校验（kind、键字符集与字节上限、非负时间），再检查单调时间（`Now < 当前 now` 返回 `ErrTime`）。
- 随后在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
- 任一 op 失败（`ErrNotFound`）或最终容量超限（`ErrCapacity`）时整体回滚：淘汰、时间与 revision 均不落盘，只有全部成功才原子提交。非空成功批次 generation 恰好 +1，空批次不变。
- `Expire(now)` 使用相同闭区间边界（`ExpiresAt <= now`），并推进表的单调时间。

### 并发与所有权
- 所有公开方法由单个 `sync.Mutex` 保护，可安全并发调用；表内状态（条目 map、now、generation、nextRevision）绝不逃逸到返回值中。

### 复杂度
- `Apply`：O(B + E)，B 为批次 op 数，E 为当前条目数（候选复制与淘汰扫描）。
- `Expire` / `Snapshot`：O(E log E)（含排序）。
- 单 op 的 Put/Touch/Delete 在候选副本上为 O(1) 均摊。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
