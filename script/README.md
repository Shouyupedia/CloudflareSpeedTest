# XIU2/CloudflareSpeedTest - Script(脚本)

这里都是一些通过调用 **CFST** 并**扩展实现更多个性化功能**的脚本。  

****
> [!TIP]
> 我之所以将 CFST 制作为一个**命令行程序**，就是考虑到**通用性**，因为毕竟不可能把所有需求都塞到软件内（特别是一些**个性化、小众**的需求），这样增加维护难度和精力不说，还会导致软件异常臃肿（`“变成我讨厌的样子”`），而命令行程序的优势之一就在于**可以很方便的和其他软件、脚本搭配使用**。

比如像下面这些我写的几个脚本，就是把一些需求以外置脚本方式实现。  

> 即脚本调用 CFST 测速并获取结果，然后***按照自己的需求自由决定***如何处理得到的测速结果（比如修改 Hosts 等）。  

总的来说，我写的这几个脚本都比较简单，功能也很单一，除了满足部分用户的需求外，***更像是一个 CFST 与脚本搭配使用的示例参考***，对于一些会写脚本、软件的用户来说，完全可以**自给自足**来实现一些个性化需求。

当然，如果你有一些自用好用的脚本也可以通过 [**Issues**](https://github.com/XIU2/CloudflareSpeedTest/issues)、[**Discussions**](https://github.com/XIU2/CloudflareSpeedTest/discussions) 或 **Pull requests** 发给我添加到这里让更多人用到！

> 小提示：点击↗右上角的三横杠图标按钮即可查看目录~

****
## 📑 cfst_hosts.sh / cfst_hosts.bat (已内置压缩包)

脚本会运行 CFST 获得最快 IP，并替换掉 Hosts 文件中的旧 CDN IP。

> **作者：**[@XIU2](https://github.com/xiu2)  
> **使用说明/问题反馈：https://github.com/XIU2/CloudflareSpeedTest/discussions/312**

<details>
<summary><code><strong>「 更新日志」</strong></code></summary>

****

#### 2026年10月11日，版本 v1.0.6 (cfst_hosts.sh / cfst_hosts_mac.sh)
 - **1. 修复** 安全问题：替换 Hosts 前检查 IP 格式，避免 nowip_hosts.txt 内容被篡改时向 sed 命令注入并以 root 权限执行任意命令  
 - **2. 修复** 替换 Hosts 时会误替换包含旧 IP 的其他 IP（如旧 IP 为 1.1.1.1 时会把 11.1.1.10 改坏）的问题，现在只替换行首完全一致的 IP  
 - **3. 修复** 没有满足条件的 IP 时会误用上次测速结果的问题  

#### 2025年12月15日，版本 v1.0.5 (cfst_hosts.bat)
 - **1. 修复** CFST新版本下获取不到第一行 IP 的问题 

#### 2021年12月17日，版本 v1.0.4
 - **1. 优化** [找不到满足条件的 IP 就一直循环测速] 功能，在指定下载测速下限时没有重新测速的问题（默认注释）   

#### 2021年12月17日，版本 v1.0.3
 - **1. 新增** 找不到满足条件的 IP 就一直循环测速功能（默认注释）  
 - **2. 优化** 代码  

#### 2021年09月29日，版本 v1.0.2
 - **1. 修复** 当测速结果 IP 数量为 0 时，脚本没有退出的问题  

#### 2021年04月29日，版本 v1.0.1
 - **1. 优化** 不再需要加上 -p 0 参数来避免回车键退出了（现在可以即显示结果，又不用担心回车键退出程序）  

#### 2021年01月28日，版本 v1.0.0
 - **1. 发布** 第一个版本  

</details>

****

## 📑 cfst_3proxy.bat (已内置压缩包)

脚本会运行 CFST 测速后获取最快 IP 并替换 3Proxy 配置文件中的旧 Cloudflare CDN IP。  
可以把所有 Cloudflare CDN IP 都重定向至最快 IP，实现一劳永逸的加速所有使用 Cloudflare CDN 的网站（不需要一个个添加域名到 Hosts 了）。

> **作者：**[@XIU2](https://github.com/xiu2)  
> **使用说明/问题反馈：https://github.com/XIU2/CloudflareSpeedTest/discussions/71**

<details>
<summary><code><strong>「 更新日志」</strong></code></summary>

****

#### 2025年12月15日，版本 v1.0.6
 - **1. 修复** CFST新版本下获取不到第一行 IP 的问题 

#### 2021年12月17日，版本 v1.0.5
 - **1. 优化** [找不到满足条件的 IP 就一直循环测速] 功能，在指定下载测速下限时没有重新测速的问题（默认注释）   

#### 2021年12月17日，版本 v1.0.4
 - **1. 新增** 找不到满足条件的 IP 就一直循环测速功能（默认注释）  
 - **2. 优化** 代码  

#### 2021年09月29日，版本 v1.0.3
 - **1. 修复** 当测速结果 IP 数量为 0 时，脚本没有退出的问题  

#### 2021年04月29日，版本 v1.0.2
 - **1. 优化** 不再需要加上 -p 0 参数来避免回车键退出了（现在可以即显示结果，又不用担心回车键退出程序）  

#### 2021年03月16日，版本 v1.0.1
 - **1. 优化** 代码及注释内容  

#### 2021年03月13日，版本 v1.0.0
 - **1. 发布** 第一个版本  

</details>

****

## 📑 cfst_dnspod.sh

如果你的域名托管在 **dnspod**，则可以通过 dnspod 官方提供的 API 来自动更新域名解析记录！  
脚本会运行 CFST 测速获得最快 IP，并通过 DNSPod API 来更新域名解析记录为这个最快 IP。

> **作者：**[@imashen](https://github.com/imashen)  
> **使用说明/问题反馈：https://github.com/XIU2/CloudflareSpeedTest/pull/533**

<details>
<summary><code><strong>「 更新日志」</strong></code></summary>

****

#### 2026年10月11日，版本 v1.0.1
 - **1. 修复** 查询解析记录时没有限定子域名，可能会修改到同域名下其他子域名（如 @、www）解析记录的问题  
 - **2. 修复** 安全问题：API Token 不再出现在进程列表（命令行参数）中被其他用户看到  
 - **3. 优化** 检查测速结果 IP 格式，并支持在定时任务中运行（切换到脚本所在目录）  

#### 2024年08月06日，版本 v1.0.0
 - **1. 发布** 第一个版本  

</details>

****

## 📑 cfst_ddns.sh / cfst_ddns.bat

如果你的域名托管在 **Cloudflare**，则可以通过 Cloudflare 官方提供的 API 来自动更新域名解析记录！  
脚本会运行 CFST 测速获得最快 IP，并通过 Cloudflare API 来更新域名解析记录为这个最快 IP。

> **作者：**[@XIU2](https://github.com/xiu2)  
> **使用说明/问题反馈：https://github.com/XIU2/CloudflareSpeedTest/discussions/481**

<details>
<summary><code><strong>「 更新日志」</strong></code></summary>

****

#### 2026年10月11日，版本 v1.0.7 (cfst_ddns.sh)
 - **1. 修复** 安全问题：API 密钥/令牌不再出现在进程列表（命令行参数）中被其他用户看到  
 - **2. 修复** 安全问题：检查配置项及测速结果 IP 格式，避免向请求数据中注入其他内容  
 - **3. 修复** 没有满足条件的 IP 时会误用上次测速结果的问题  

#### 2025年12月15日，版本 v1.0.6 (cfst_ddns.bat)
 - **1. 修复** CFST新版本下获取不到第一行 IP 的问题 

#### 2024年10月06日，版本 v1.0.5
 - **1. 新增** 支持 API 令牌方式（相比 API 密钥这种全局权限的，API 令牌可以自由控制权限）   

#### 2021年12月17日，版本 v1.0.4
 - **1. 新增** 找不到满足条件的 IP 就一直循环测速功能（默认注释）  
 - **2. 优化** 代码  

#### 2021年09月29日，版本 v1.0.3
 - **1. 修复** 当测速结果 IP 数量为 0 时，脚本没有退出的问题  

#### 2021年04月29日，版本 v1.0.2
 - **1. 优化** 不再需要加上 -p 0 参数来避免回车键退出了（现在可以即显示结果，又不用担心回车键退出程序）  

#### 2021年01月27日，版本 v1.0.1
 - **1. 优化** 配置从文件中读取  

#### 2021年01月26日，版本 v1.0.0
 - **1. 发布** 第一个版本  

</details>

****

## 📑 cfst_cfddns.sh

适用于 **Linux** 的 Cloudflare DDNS 脚本（相当于 `cfst_ddns.sh` 的增强版）：运行 CFST 测速获得最快 IP，并通过 Cloudflare API 同步到域名解析记录。

 - 只需填写 **根域名 + 完整域名 + API 令牌**，自动查询区域 ID、解析记录 ID，记录不存在时自动创建
 - 支持同时更新 **A（IPv4）+ AAAA（IPv6）** 记录，IP 没有变化时不会重复更新
 - 支持 `-httping-url`：使用你的网站地址进行 HTTPing，过滤掉返回无效状态码或 **Cloudflare 1034 错误**（该 IP 不能用于访问你的网站）的 IP
 - 支持 `-durl`：单独指定下载测速地址；下载测速全部失败（速度为 0.00）时不会更新解析记录
 - API 令牌不会出现在进程列表中；配置文件只解析不执行；测速结果保存在私有临时目录；可防止定时任务重叠运行

> **依赖：** bash 4+、curl、jq（如 `apt install curl jq`、`yum install curl jq`、OpenWrt 上 `opkg install bash curl jq`），以及支持 `-durl`、`-httping-url` 参数的 CFST

<details>
<summary><code><strong>「 使用说明」</strong></code></summary>

****

1. 将 `cfst_cfddns.sh`、`cfst_cfddns.conf.example` 放到 CFST 所在目录（和 `cfst`、`ip.txt`、`ipv6.txt` 同一个目录）。
2. 在 Cloudflare 后台 [创建 API 令牌](https://dash.cloudflare.com/profile/api-tokens)，权限为 **区域(Zone) - DNS - 编辑(Edit)**（如果不填写区域 ID，还需要 **区域(Zone) - 区域(Zone) - 读取(Read)**），区域资源选择你的域名。
3. 复制配置文件并修改（配置项说明见配置文件中的注释）：

```bash
cp cfst_cfddns.conf.example cfst_cfddns.conf
chmod 600 cfst_cfddns.conf    # 配置文件中有 API 令牌，不要让其他用户读取
vi cfst_cfddns.conf           # 填写 CF_API_TOKEN、CF_ZONE_NAME、CF_RECORD_NAME 等
chmod +x cfst_cfddns.sh cfst
```

4. 先试运行（只测速并显示将要更新的 IP，不会修改解析记录），确认没有问题后再正式运行：

```bash
./cfst_cfddns.sh -n                     # 试运行
./cfst_cfddns.sh                        # 正式运行
./cfst_cfddns.sh -f                     # 强制更新（即使 IP 没有变化，如修改了 TTL、代理状态后）
./cfst_cfddns.sh -c /path/to/other.conf # 指定其他配置文件
```

5. 添加定时任务（`crontab -e`），例如每 6 小时运行一次（路径请改为实际路径）：

```bash
0 */6 * * * /opt/cfst/cfst_cfddns.sh >> /opt/cfst/cfst_cfddns.log 2>&1
```

> **提示：** 建议把 `CFST_HTTPING_URL` 填写为你实际要通过优选 IP 访问的网站地址（如 `https://www.example.com/cdn-cgi/trace`），这样会使用 HTTPing 模式测速，并过滤掉返回 Cloudflare 1034 错误（Edge IP Restricted）的 IP，避免把不能访问你网站的 IP 同步到解析记录。  
> **提示：** 脚本不需要 root 权限，建议使用普通用户运行。已更新或无需更新时退出码为 0，没有找到满足条件的 IP、API 出错时退出码为 1。

</details>

<details>
<summary><code><strong>「 更新日志」</strong></code></summary>

****

#### 2026年10月11日，版本 v1.0.0
 - **1. 发布** 第一个版本  

</details>

****

## 📑 cfst_dnsmasq.sh

脚本会运行 CFST 测速后获取最快 IP 并替换 dnsmasq 配置文件中的旧 Cloudflare CDN IP。  

> **作者：**[@Sving1024](https://github.com/Sving1024)  
> **使用说明/问题反馈：https://github.com/XIU2/CloudflareSpeedTest/discussions/566**

<details>
<summary><code><strong>「 更新日志」</strong></code></summary>

****

#### 2026年10月11日，版本 v1.0.2
 - **1. 修复** 安全问题：检查 site.conf 中的域名及测速结果 IP 格式，避免向 dnsmasq 配置文件写入其他配置  
 - **2. 修复** 没有满足条件的 IP 时会误用上次测速结果的问题，并支持在定时任务中运行（切换到脚本所在目录）  

#### 2025年01月22日，版本 v1.0.1
 - **1. 修复** IPv6 的问题  

#### 2024年12月28日，版本 v1.0.0
 - **1. 发布** 第一个版本  

</details>

****

## 功能建议/问题反馈

如果这些脚本使用过程中你遇到了什么问题，可以先去脚本对应的 **`使用说明`** 帖子里看看是否有别人问过了。  
如果没找到类似问题，那么就在脚本对应的 **`使用说明`** 帖子里直接评论问作者吧。