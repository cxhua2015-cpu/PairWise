# pipelinegraph

并发安全的内存型有向无环“流水线依赖图”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引结构

图内部维护三份索引，均在 `sync.RWMutex` 保护之下：

- `nodes map[string]struct{}`：节点集合，O(1) 存在性判断。
- `out map[string]map[string]struct{}`：正向邻接表（from → to 集合），用于可达性 DFS 与环检测。
- `in map[string]map[string]struct{}`：反向邻接表（to → from 集合），使 `DeleteNode` 能 O(度数) 清理入边，无需全图扫描。

正反向索引始终同步更新，删除节点时同时摘除其所有关联边。

### 候选事务（candidate transaction）

`Apply` 先在持锁前对整个批次做纯结构校验（kind 合法、名称字符集与字节上限、节点操作不得携带 `To`），不读取任何状态。随后获取写锁，把 `nodes`/`out`/`in` 深拷贝为候选副本，在副本上顺序执行全部操作（存在性、环检测、重复边检查均针对副本）。节点/边容量只在批次末尾对候选结果检查一次，因此“先删后增”的批次可以合法通过。任一步失败直接丢弃副本返回错误，已提交状态与 `generation` 完全不变（整体回滚）；全部成功才一次性替换内部状态并将 `generation` 加一。空批次不修改状态、不增加 generation。

### 所有权与并发

- 所有公开方法可并发调用：写操作（`Apply`）持写锁，读操作（`Reachable`、`Snapshot`）持读锁，读者之间互不阻塞。
- `Snapshot` 与 `Result` 返回的切片均为新建副本，调用方修改返回值不会影响内部状态；内部也从不保留调用方传入的切片。
- `Reachable` 在读锁内对当前一致快照做 DFS，不会观察到批次中间态。

### 复杂度

设 N 为节点数、E 为边数、B 为批次操作数、d 为被删节点的度数：

- `Apply`：拷贝 O(N+E)，执行 O(B·(N+E))（每次加边的环检测为一次 DFS），容量检查 O(N+E)。
- `Reachable`：O(N+E)。
- `Snapshot`：O(N+E) 收集，排序 O(N log N + E log E)。
- `DeleteNode` 单操作：O(d)。
- 空间：O(N+E)。
