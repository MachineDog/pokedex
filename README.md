# PokéDeck

使用 Go 和 PokéAPI 构建的中文宝可梦图鉴，包含无限滚动列表、宝可梦详情、多图缓存与招式数据。

## 构建与运行

```bash
make
make run
```

`make run` 会以系统用户 `pokedeck` 运行本地构建，以便与已安装服务安全共用缓存。如果 `pokedeck.service` 正在运行，它会先被停止，并在本地程序退出或被中断后自动恢复；服务原本未运行时则不会自动启动。

访问 <http://localhost>。

## 缓存

API 数据存入 `/var/lib/pokedeck/pokedeck.db` SQLite 数据库，图片持久化到 `/var/lib/pokedeck/cache`，本地测试和 Deb 服务共用这些数据。数据库按 URL 保存原始 JSON，并记录资源类型、资源名称和更新时间，方便后续建立检索。旧文件缓存中的资源再次被访问时会自动迁移到数据库，不会重新请求 API。

图片统一通过 `/media/image?src=...` 提供，页面不直接依赖图片 CDN，后续可以在该接口后接本地存储或对象存储。

应用启动后会扫描全部宝可梦，把所有尚未缓存的图片加入普通优先级队列。用户正在查看的图片会进入高优先级队列；如果它已经排在普通队列中，会被提升到队首优先处理。两个后台 worker 始终优先处理用户请求，且相同资源只会下载一次。

图片请求不会同步访问 CDN。缓存未命中时媒体接口会立即返回占位图并将下载任务加入独立的后台 goroutine 队列；页面会自动检查缓存状态并在图片就绪后替换占位图，因此图片源缓慢或失败不会阻塞列表浏览。

点击任意宝可梦卡片会在首页内打开详情层，首页 DOM、无限滚动进度和滚动位置都会保留；浏览器返回键只关闭详情层。直接访问 `/pokemon/{name}` 也会返回同一个首页框架并自动打开详情。详情包含相邻编号切换、图鉴描述、分类、身高体重、种族值、特性、栖息地、捕获率等资料。官方立绘、当前正背面、闪光、雌性、Pokémon HOME、Dream World 和 Showdown 图像进入后台持久化缓存；`sprites.versions` 中数量较大的历代游戏图像直接使用原始链接，不占用本地缓存。程序启动时会根据本地 API 数据自动删除以前缓存的历代图像。

招式通过普通 HTTP 接口 `/api/pokemon/{name}/moves?offset=0&limit=24` 分批加载，不使用 WebSocket。响应包含中文名称、属性、伤害分类、威力、命中率、PP、学习方式和学习等级，并沿用 `cache.data_ttl` 落盘缓存。

顶部导航固定显示搜索框和语言选择。语言列表以及宝可梦名称、描述、分类、属性、特性、种族值和招式等翻译均来自 PokéAPI；当前 API 返回的语言会自动加入选择框。`/api/search` 支持任意已索引语言、编号、部分名称和少量拼写误差；搜索结果和详情都通过普通 HTTP 获取，不刷新首页。切换语言时只更新浏览器已有数据，应用自身的界面文字目前提供中、英、日三套，其他语言回退英文。

## 配置

应用配置在 `config.yaml` 中：

```yaml
server:
  address: "[::]:80"
cache:
  directory: "/var/lib/pokedeck/cache"
  database: "/var/lib/pokedeck/pokedeck.db"
  # 开发时允许数据库缺失记录回源；离线发布设为 false。
  api_fallback: true
  # 0 表示永不过期，也可以设置为 "720h" 等有效时长。
  data_ttl: "0"
  image_ttl: "0"
precache:
  enabled: true
  delay: "10s"
  scan_workers: 8
  image_workers: 2
ui:
  batch_size: 24
```

页面使用无限滚动，不再显示分页控件。`ui.batch_size` 控制首次显示及每次接近底部时追加的卡片数量，可设置为 4–100，默认 24。批量接口只读取内存索引和本地缓存，不等待 PokéAPI 或图片 CDN。

`cache.data_ttl` 和 `cache.image_ttl` 为 `"0"` 时数据永不过期。`cache.api_fallback: false` 时，数据库缺失记录会直接报错，程序不会调用 PokéAPI。

## Debian 包与服务

```bash
make deb VERSION=0.1.0
sudo apt install ./build/pokedeck_0.1.0_$(dpkg --print-architecture).deb
```

`make deb` 会把 `DB_SOURCE`（默认 `data/pokedeck.db`）放进安装包。安装时仅在数据目录尚无数据库时复制种子库，因此升级不会覆盖运行期间积累的数据。构建不允许调用 PokéAPI 的发布包时使用：

```bash
make deb-offline DB_SOURCE=/path/to/complete-pokedeck.db VERSION=0.1.0
```

该目标会验证数据库非空，并把包内 `api_fallback` 改为 `false`。正式发布前仍需用完整预热库作为 `DB_SOURCE`，普通开发包保持允许补齐缺失数据。

安装包会创建 `pokedeck` 系统用户、安装并启用 `pokedeck.service`，服务默认在所有 IPv4/IPv6 地址的 80 端口监听，缓存位于 `/var/lib/pokedeck/cache`。服务配置统一位于 `/etc/pokedeck/config.yaml`；修改 `cache.directory` 即可指定数据目录。自定义目录需预先创建并授予 `pokedeck` 用户写权限。修改后执行：

```bash
sudo systemctl daemon-reload
sudo systemctl restart pokedeck
```

服务配置位于 `/etc/pokedeck/config.yaml`，可通过 `systemctl enable|disable pokedeck` 控制开机自启。

## 项目结构

- `cmd/pokedeck/`：应用源码与测试
  - `bootstrap.go`：读取配置、组装依赖并启动 HTTP 服务
  - `api_database.go`：SQLite API 数据库、索引和离线读取
  - `config.go`：配置模型、校验与默认配置
  - `pokemon_models.go` / `detail_models.go`：数据模型
  - `detail_service.go`：详情资料、多图收集与招式 HTTP 接口
  - `index_page.go`：首页、详情层、实时多语言和前端状态管理
  - `search.go`：多语言模糊搜索和内存索引
  - `main.go`：缓存、图鉴服务、Web 处理器和图片优先级队列
  - `main_test.go`：缓存队列、图片变体和招式接口测试
- `config.yaml`：本地运行配置
- `packaging/`：Debian 包、systemd 服务及安装维护脚本
- `Makefile`：构建、测试、安装和 Deb 打包入口
