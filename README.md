# スペースシューター

リアルタイム2Dマルチプレイヤーシューティングゲーム

## 概要

「スペースシューター」は複数プレイヤーが宇宙船を操作し、敵を倒すシンプルな2Dシューティングゲームです。WebSocketを使用したリアルタイム通信により、複数のプレイヤーが同時にプレイできます。

## 特徴

- リアルタイムマルチプレイヤー対応
- WebSocket通信による低遅延ゲームプレイ
- シンプルな操作性
- 自動生成される敵
- スコアとヘルスポイント管理

## 技術スタック

- **バックエンド**: Go + Echo フレームワーク
- **通信**: WebSocket (gorilla/websocket)
- **フロントエンド**: HTML5 Canvas + JavaScript
- **データ保存**: インメモリ

## インストール方法

### 前提条件

- Go 1.14以上
- git

### 手順

1. リポジトリをクローン:

```bash
git clone https://github.com/tacyan/space-shooter.git
cd space-shooter
```

2. 必要なパッケージをインストール:

```bash
go get github.com/labstack/echo/v4
go get github.com/gorilla/websocket
go get github.com/google/uuid
```

3. サーバーを起動:

```bash
go run main.go
```

4. ブラウザでアクセス:

```
http://localhost:1323
```

## 操作方法

- **移動**: 矢印キー または WASD
- **射撃**: スペースキー

## プロジェクト構造

```
.
├── main.go        # バックエンドコード
├── public/        # フロントエンドファイル
│   └── index.html # ゲームのHTMLとJavaScript
└── README.md      # このドキュメント
```

## ゲームルール

- 他のプレイヤーと協力して敵を倒します
- 敵を倒すと10ポイント獲得
- 敵と衝突すると体力が10減少
- 体力が0になるとリスポーンし、50ポイント減少

## 拡張アイデア

- 異なる種類の敵
- パワーアップアイテム
- 複数レベル
- 永続的なハイスコア
- チャット機能

## ライセンス

MIT

## 作者

[あなたの名前]
