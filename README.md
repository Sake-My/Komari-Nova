# Komari

![Badge](https://hitscounter.dev/api/hit?url=https%3A%2F%2Fgithub.com%2FSake-My%2FKomari-Nova&label=&icon=github&color=%23a370f7&message=&style=flat&tz=UTC)

![komari](https://socialify.git.ci/Sake-My/Komari-Nova/image?description=1&font=Inter&forks=1&issues=1&language=1&logo=https%3A%2F%2Fraw.githubusercontent.com%2Fkomari-monitor%2Fkomari-web%2Fd54ce1288df41ead08aa19f8700186e68028a889%2Fpublic%2Ffavicon.png&name=1&owner=1&pattern=Plus&pulls=1&stargazers=1&theme=Auto)

[English](./README.md) | [简体中文](./README_zh-cn.md)

Komari is a lightweight, self-hosted server monitoring solution. It provides a simple and efficient way to track server performance through a web interface, with metrics collected by a lightweight agent.

> [!WARNING]
> Komari is a self-hosted monitoring and control application. Deploy it only on systems you own or are authorized to manage. You are solely responsible for how you deploy and use Komari. The developers accept no liability for unauthorized access, persistence, command execution, other misuse, or any resulting consequences.

This repository is maintained at [Sake-My/Komari-Nova](https://github.com/Sake-My/Komari-Nova), based on [komari-monitor/komari](https://github.com/komari-monitor/komari). The program name remains `komari`; the frontend currently uses [komari-monitor/komari-web](https://github.com/komari-monitor/komari-web).

[Upstream documentation (general reference)](https://www.komari.wiki/) — deployment URLs and release assets for this repository are listed below.

## Features

- **Real-time monitoring**: Displays monitoring data at one-second intervals.
- **Lightweight and efficient**: Uses minimal system resources and works well on servers of any size.
- **Self-hosted**: Keeps you in control of your data and privacy.
- **Web interface**: Provides an intuitive, easy-to-use monitoring dashboard.
- **Extensible**: Supports custom themes and plugins.

## Quick Start

Download server binaries from [this repository's Releases](https://github.com/Sake-My/Komari-Nova/releases). On Linux, use [install-komari.sh](https://github.com/Sake-My/Komari-Nova/blob/main/install-komari.sh) for installation and service management.

Docker deployment:

```bash
docker run -d --name komari --restart unless-stopped \
  -p 25774:25774 \
  -v "$(pwd)/data:/app/data" \
  ghcr.io/sake-my/komari-nova:latest
```

Open `http://<server-address>:25774` after startup. Release binaries and container images become available only after this repository publishes its first release and completes the corresponding build workflows.

## Screenshots

| Page                | Screenshot                                                                                                                                                             |
| ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Home Dashboard      | <img src="https://b2.akz.moe/awesome-pictures/komari-screenshot/%E4%B8%BB%E9%A1%B5%E4%BB%AA%E8%A1%A8%E7%9B%98-en.webp" width="800" alt="Home Dashboard">               |
| Admin Dashboard     | <img src="https://b2.akz.moe/awesome-pictures/komari-screenshot/%E5%90%8E%E5%8F%B0%E4%BB%AA%E8%A1%A8%E7%9B%98-en.webp" width="800" alt="Admin Dashboard">              |
| History Charts      | <img src="https://b2.akz.moe/awesome-pictures/komari-screenshot/%E5%8E%86%E5%8F%B2%E5%9B%BE%E8%A1%A8-en.webp" width="800" alt="History Charts">                        |
| Web Terminal        | <img src="https://b2.akz.moe/awesome-pictures/komari-screenshot/%E7%BD%91%E9%A1%B5%E7%BB%88%E7%AB%AF.webp" width="800" alt="Web Terminal">                             |
| Customizable Themes | <img src="https://b2.akz.moe/awesome-pictures/komari-screenshot/%E4%B8%BB%E9%A2%98%E5%8F%AF%E8%87%AA%E5%AE%9A%E4%B9%89-en.webp" width="800" alt="Customizable Themes"> |
| Theme Market        | <img src="https://b2.akz.moe/awesome-pictures/komari-screenshot/%E4%B8%BB%E9%A2%98%E5%B8%82%E5%9C%BA-en.webp" width="800" alt="Theme Market">                          |

## Upstream Contributors

Thanks to the original Komari authors and everyone who contributed to the upstream project. The contributor list below belongs to [komari-monitor/komari](https://github.com/komari-monitor/komari). Original copyright and license notices are retained in [LICENSE](./LICENSE) and [NOTICE](./NOTICE).

<a href="https://github.com/komari-monitor/komari/graphs/contributors"><img src="https://contributors-img.web.app/image?repo=komari-monitor/komari" alt="Komari contributors" width="600"></a>
