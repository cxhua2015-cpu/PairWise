# expirytable339

并发安全的内存型“到期状态表”，语义见 `SPEC.md`。仅依赖标准库，Go 1.22+。

## 设计说明

- **索引**：主索引为 `map[string]Entry`，键即条目键，Put/Touch/Delete 与最终容量检查均为 O(1) 均摊；到期淘汰与 `Expire` 为全表扫描 O(n)。`Snapshot` 返回按键排序的副本，排序 O(n log n)。
- **候选事务**：`Apply` 先在锁外对整个批次做结构校验（kind、键字符集与字节上限、非负 `ExpiresAt`），再在锁内检查单调时间；随后克隆当前 map 得到候选状态，先在候选上删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete 并分配 revision。容量超限或任何错误直接丢弃候选，淘汰、时间与 revision 一并回滚，已提交状态不受污染。
- **所有权**：表内部条目绝不外借。`Expire` 与 `Snapshot` 返回的切片均为新建副本，调用方修改不影响内部状态；返回值不包含内部 map 的任何引用。
- **并发**：所有公开方法共用一把 `sync.Mutex`，批次整体串行化，保证单调时间、revision 与 generation 的全序一致性。`New` 之后 `Options` 派生的上限不可变，可无锁读取。
- **复杂度**：设批次含 m 个 op、表内 n 个条目，单次 `Apply` 为 O(n + m)（克隆 + 淘汰扫描 + 顺序执行）；`Expire` 为 O(n + k log k)（k 为到期条目数，仅排序返回切片）；`Snapshot` 为 O(n log n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
