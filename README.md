# expirytable374

并发安全的内存型“到期状态表 374”（Go 1.22+，仅标准库）。详细语义见 `SPEC.md`。

## 索引结构

- 主索引为 `map[string]Entry`，键到条目 O(1) 定位；`Snapshot`/`Expire` 返回前按键排序，保证确定性输出。
- 全表由一把 `sync.Mutex` 保护，所有公开方法（`Apply`/`Expire`/`Snapshot`）可并发调用；`New` 中的选项校验不依赖锁。

## 候选事务（Apply）

1. **结构校验**：在加锁前完整校验整个批次（未知 kind、空键、非法字符、超长键 → `ErrInvalidInput`），不读取任何状态。
2. **时间检查**：`Now` 必须非负且不早于当前表时间，否则 `ErrTime`。
3. **候选状态**：复制当前条目，先淘汰 `ExpiresAt <= Now`（闭区间）的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从单调计数器分配 revision，Touch/Delete 目标缺失 → `ErrNotFound`。
4. **提交或回滚**：最终条目数超过 `MaxEntries` → `ErrCapacity`。任何错误都整体回滚——淘汰、时间和 revision 均不落盘；成功时原子替换表内容、推进 `Now`，非空批次 generation 恰好 +1，空批次 generation 不变。

`Expire(now)` 使用相同的闭区间边界（`ExpiresAt <= now`）淘汰并返回条目，同时推进表时间；时间回退同样返回 `ErrTime`。

## 所有权

- 返回的 `[]Entry`（`Expire`、`Snapshot.Entries`）均为新分配的切片与条目副本，调用方可自由修改，不影响内部状态。
- 键与条目按值拷贝存入，表不保留调用方切片或字符串以外的引用。

## 复杂度

- `Apply`：O(E + O)，E 为当前条目数（候选复制 + 淘汰扫描），O 为批次数。
- `Expire`：O(E + R log R)，R 为被淘汰条目数（排序）。
- `Snapshot`：O(E log E)（排序）。
- 空间：O(E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
