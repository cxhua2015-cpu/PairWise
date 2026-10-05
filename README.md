# resourcecatalog091

并发安全的内存型资源目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行 Put/Delete，失败整体回滚。

## 设计说明

- **索引**：`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；另维护 `totalVal`（Value 总字节数）与 `nextRev`（下一个待分配 revision）两个派生计数，避免批次末容量检查时的全表扫描。Snapshot/Result 的排序输出在读取时按名称 `sort.Strings` 生成，不维护额外有序结构。
- **候选事务**：`Apply` 先对整个批次做完整结构校验（kind、名称字符集与长度、Value 长度、Delete 不得携带 Value），不触碰任何状态；随后克隆当前 map 得到候选状态，按输入顺序在候选上应用全部操作（Put 分配连续 revision，Delete 不分配），批次末才检查记录数与 Value 总字节容量。任一环节失败直接丢弃候选，generation、revision 与记录全部保持原样；成功时一次性提交并将 generation 加一（空批次不加）。
- **所有权**：Put 时深拷贝调用方传入的 Value；Get、Snapshot、Result.Changed 返回的 Value 均为独立副本。调用方之后修改入参或返回值切片，均不影响目录内部状态，反之亦然。
- **并发**：所有公开方法共用一把 `sync.Mutex`，Apply 的校验-执行-提交整体串行化，Get/Snapshot 与 Apply 互斥，返回切片与内部状态完全隔离。

## 复杂度

- `New`：O(1)。
- `Apply`（n 个操作、m 条现存记录）：克隆候选 O(m)，执行 O(n)，排序 Changed O(n log n)，合计 O(m + n log n)，额外空间 O(m + n)。
- `Get`：O(1) 查询 + O(v) 拷贝（v 为 Value 长度）。
- `Snapshot`：O(m log m) 排序 + O(总字节数) 深拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
