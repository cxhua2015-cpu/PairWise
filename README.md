# topologygraph208

并发安全的内存型控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引结构

`Graph` 内部持有单一不可共享的 `state`，包含三张表：

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `edges map[Edge]struct{}`：边集合（`Edge{From,To}` 为可比较键），O(1) 去重与删除。
- `adj map[string]map[string]struct{}`：出边邻接表，供可达性 BFS 与环检测使用；删除节点时反向清理通过遍历邻接表完成。

节点/边容量与名称字节上限保存在 `Graph` 的只读字段中，`New` 时校验（必须为正，否则 `ErrInvalidOptions`）。

## 候选事务（candidate transaction）

`Apply` 采用“候选事务”模型：

1. 在写锁内先对整个批次做**纯结构校验**（kind 合法、无多余字段、名称字符集与长度合法），不读取任何图状态；任一 op 非法即返回 `ErrInvalidInput`。
2. 将当前 `state` 深拷贝为候选状态，所有 op 顺序作用于候选状态；任何 `ErrExists`/`ErrNotFound`/`ErrCycle` 直接丢弃候选，原状态不受影响。
3. 仅在批次末尾检查最终节点数/边数是否超出容量，超限返回 `ErrCapacity` 并整体回滚（丢弃候选）。
4. 全部成功才用候选状态原子替换当前状态，且非空成功批次 `generation` 恰好 +1；空批次与失败批次不改变 generation。

## 所有权与并发

- 所有公开方法（`Apply`/`Reachable`/`Snapshot`）通过一把 `sync.RWMutex` 保护：写操作独占，读操作可并发。
- `Snapshot` 返回的 `Nodes`/`Edges` 切片为每次调用新建并排序（节点按字典序，边按 `(From,To)` 字典序），调用方修改返回值不会影响内部状态；候选状态在提交前对外不可见，提交后旧状态不再被改写。
- 名称仅允许非空 ASCII 小写字母、数字、`-`、`_`，且不超过 `MaxNameBytes`。

## 复杂度

设 `N` 为节点数、`E` 为边数、批次含 `K` 个 op：

- `Apply`：结构校验 O(K·L)（L 为名称长度）；候选拷贝 O(N+E)；每个 `AddEdge` 的环检测为一次 BFS，O(N+E)；整体 O(K·(N+E))。
- `Reachable`：一次 BFS，O(N+E)。
- `Snapshot`：收集 O(N+E)，排序 O(N log N + E log E)。
- 空间：O(N+E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
