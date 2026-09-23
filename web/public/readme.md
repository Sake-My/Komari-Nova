# Frontend Build Instructions / 前端构建说明 / フロントエンド構築手順

## English

### Frontend Repository

- **Frontend project repository**: https://github.com/Sake-My/Komari-Nova-Web

### Build Requirements

1. Clone the frontend repository and build the static files
2. Pack the generated `dist` directory as `tar + zstd -19` to `web/public/defaultTheme/dist.tar.zst` in the backend repository
3. Copy `komari-theme.json` to `web/public/defaultTheme`; this file is required by the backend's `go:embed` directive, and the backend will not compile without it
4. Ensure `web/public/defaultTheme/dist.tar.zst` contains `index.html` before building the backend

### Important Note

The backend is maintained at https://github.com/Sake-My/Komari-Nova. The frontend is maintained at https://github.com/Sake-My/Komari-Nova-Web.

---

## 中文

### 前端项目仓库

- **前端项目地址**: https://github.com/Sake-My/Komari-Nova-Web

### 构建要求

1. 克隆前端仓库并构建静态文件
2. 将生成的 `dist` 目录使用 `tar + zstd -19` 打包为后端仓库内的 `web/public/defaultTheme/dist.tar.zst`
3. 将 `komari-theme.json` 复制到 `web/public/defaultTheme`；该文件由后端 `go:embed` 引用，缺少它会导致后端编译失败
4. 构建后端前，确保 `web/public/defaultTheme/dist.tar.zst` 包含 `index.html`

### 重要提醒

后端维护仓库为 https://github.com/Sake-My/Komari-Nova，前端维护仓库为 https://github.com/Sake-My/Komari-Nova-Web。

---

## 日本語

### フロントエンドプロジェクトリポジトリ

- **フロントエンドプロジェクトアドレス**: https://github.com/Sake-My/Komari-Nova-Web

### ビルド要件

1. フロントエンドリポジトリをクローンして静的ファイルをビルドする
2. 生成された `dist` ディレクトリを `tar + zstd -19` で圧縮し、バックエンドリポジトリ内の `web/public/defaultTheme/dist.tar.zst` に配置する
3. `komari-theme.json` を `web/public/defaultTheme` にコピーする。このファイルはバックエンドの `go:embed` で必須のため、存在しないとコンパイルに失敗する
4. バックエンドをビルドする前に、`web/public/defaultTheme/dist.tar.zst` に `index.html` が含まれていることを確認する

### 重要な注意事項

バックエンドの保守用リポジトリは https://github.com/Sake-My/Komari-Nova。フロントエンドの保守用リポジトリは https://github.com/Sake-My/Komari-Nova-Web を使用する。

---

## Quick Setup / 快速设置 / クイックセットアップ

```bash
# Clone frontend repository / 克隆前端仓库 / フロントエンドリポジトリをクローン
git clone https://github.com/Sake-My/Komari-Nova-Web komari-web
cd komari-web

# Install dependencies and build / 安装依赖并构建 / 依存関係をインストールしてビルド
npm install
npm run build

# Pack frontend assets into the backend embed archive / 打包到后端 embed 归档 / バックエンドの embed アーカイブに圧縮
mkdir -p /path/to/komari/web/public/defaultTheme
tar -cf /tmp/komari-dist.tar -C dist .
zstd -19 -T0 -f /tmp/komari-dist.tar -o /path/to/komari/web/public/defaultTheme/dist.tar.zst
rm -f /tmp/komari-dist.tar
cp komari-theme.json /path/to/komari/web/public/defaultTheme/
```
