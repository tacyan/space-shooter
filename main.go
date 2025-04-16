/**
 * @file main.go
 * @description リアルタイム2Dマルチプレイヤーシューティングゲーム「スペースシューター」のバックエンド
 * @author Claude
 * @version 1.0
 *
 * 概要:
 * - WebSocketを使用したリアルタイム通信
 * - 複数プレイヤーが参加可能なゲームルーム管理
 * - 敵の自動生成と衝突検出
 * - 60FPSでのゲームループ処理
 *
 * 制限事項:
 * - データの永続化は行わない（インメモリ）
 * - 最大4人までのプレイヤー
 *
 * 必要なパッケージのインストール:
 * - go get github.com/labstack/echo/v4
 * - go get github.com/gorilla/websocket
 * - go get github.com/google/uuid
 */

package main

import (
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// クライアント管理用マップ
var clients = make(map[string]*Client)
var clientsMutex sync.Mutex

var (
	// WebSocketアップグレーダー
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // 開発環境では全てのオリジンを許可
		},
	}
	// ゲームルームの管理マップ
	gameRooms = make(map[string]*GameRoom)
	// ゲームルームへの同時アクセスを防ぐためのミューテックス
	gamesMutex sync.Mutex
)

/**
 * エンティティ構造体
 * ゲーム内の全てのオブジェクト（プレイヤー、弾、敵）の基本情報
 * @property {string} ID - エンティティの一意識別子
 * @property {string} Type - エンティティの種類（"player", "bullet", "enemy"）
 * @property {float64} X - X座標位置
 * @property {float64} Y - Y座標位置
 * @property {float64} VelocityX - X方向の速度
 * @property {float64} VelocityY - Y方向の速度
 * @property {int} Width - 幅（ピクセル）
 * @property {int} Height - 高さ（ピクセル）
 */
type Entity struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	VelocityX float64 `json:"velocityX"`
	VelocityY float64 `json:"velocityY"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
}

/**
 * プレイヤー構造体
 * プレイヤー固有の情報を保持
 * @property {Entity} Entity - 基本エンティティ情報（継承）
 * @property {string} Name - プレイヤー名
 * @property {int} Score - スコア
 * @property {int} Health - 体力値
 * @property {string} Color - プレイヤーカラー（16進数カラーコード）
 */
type Player struct {
	Entity
	Name   string `json:"name"`
	Score  int    `json:"score"`
	Health int    `json:"health"`
	Color  string `json:"color"`
}

/**
 * ゲームルーム構造体
 * 一つのゲームインスタンスを表す
 * @property {string} ID - ルームの一意識別子
 * @property {map[string]*Player} Players - プレイヤーマップ（キー：プレイヤーID）
 * @property {map[string]*Entity} Bullets - 弾のマップ（キー：弾ID）
 * @property {map[string]*Entity} Enemies - 敵のマップ（キー：敵ID）
 * @property {time.Time} LastTick - 最後のゲームティック時間
 * @property {sync.Mutex} Mutex - 同時アクセス防止のミューテックス
 */
type GameRoom struct {
	ID       string             `json:"id"`
	Players  map[string]*Player `json:"players"`
	Bullets  map[string]*Entity `json:"bullets"`
	Enemies  map[string]*Entity `json:"enemies"`
	LastTick time.Time
	Mutex    sync.Mutex
}

/**
 * クライアント構造体
 * WebSocket接続しているクライアント情報
 * @property {string} ID - クライアントの一意識別子
 * @property {*websocket.Conn} Socket - WebSocketコネクション
 * @property {*GameRoom} GameRoom - 参加中のゲームルーム
 * @property {*Player} Player - 対応するプレイヤー情報
 */
type Client struct {
	ID       string
	Socket   *websocket.Conn
	GameRoom *GameRoom
	Player   *Player
}

/**
 * WebSocketメッセージ構造体
 * クライアント-サーバー間の通信形式
 * @property {string} Type - メッセージタイプ（"init", "move", "shoot", "gameState"など）
 * @property {interface{}} Data - メッセージデータ（タイプにより内容が異なる）
 */
type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

/**
 * 新規ゲームルームを作成する
 * @returns {*GameRoom} - 作成されたゲームルームへのポインタ
 */
func newGameRoom() *GameRoom {
	return &GameRoom{
		ID:       uuid.New().String(),
		Players:  make(map[string]*Player),
		Bullets:  make(map[string]*Entity),
		Enemies:  make(map[string]*Entity),
		LastTick: time.Now(),
	}
}

/**
 * メイン関数
 * サーバーの起動と初期設定を行う
 */
func main() {
	// 乱数シードの初期化
	rand.Seed(time.Now().UnixNano())

	// Echoフレームワークの初期化
	e := echo.New()

	// ミドルウェア設定
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// 静的ファイル配信
	e.Static("/", "public")

	// WebSocketエンドポイント
	e.GET("/ws", handleWebSocket)

	// サーバー起動（ポート1323）
	e.Logger.Fatal(e.Start(":1323"))
}

/**
 * WebSocket接続ハンドラー
 * クライアントからのWebSocket接続を処理する
 * @param {echo.Context} c - Echoコンテキスト
 * @returns {error} - エラー（あれば）
 */
func handleWebSocket(c echo.Context) error {
	// WebSocketへのアップグレード
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		log.Println("WebSocketアップグレードエラー:", err)
		return err
	}
	defer ws.Close()

	// クライアント作成
	clientID := uuid.New().String()
	client := &Client{
		ID:     clientID,
		Socket: ws,
	}

	// クライアント管理に追加
	clientsMutex.Lock()
	clients[clientID] = client
	clientsMutex.Unlock()

	// プレイヤー作成（ランダム色と初期位置）
	playerColors := []string{"#FF0000", "#00FF00", "#0000FF", "#FFFF00", "#FF00FF"}
	player := &Player{
		Entity: Entity{
			ID:        clientID,
			Type:      "player",
			X:         float64(300 + rand.Intn(300)),
			Y:         float64(300 + rand.Intn(300)),
			VelocityX: 0,
			VelocityY: 0,
			Width:     30,
			Height:    30,
		},
		Name:   "Player-" + clientID[:5],
		Score:  0,
		Health: 100,
		Color:  playerColors[rand.Intn(len(playerColors))],
	}
	client.Player = player

	// ゲームルーム検索・作成
	gamesMutex.Lock()
	var gameRoom *GameRoom

	// 空きのあるルームを探す
	for _, room := range gameRooms {
		if len(room.Players) < 4 { // 最大4人
			gameRoom = room
			break
		}
	}

	// 空きがなければ新規ルーム作成
	if gameRoom == nil {
		gameRoom = newGameRoom()
		gameRooms[gameRoom.ID] = gameRoom
		go gameLoop(gameRoom) // ゲームループ開始
	}
	gamesMutex.Unlock()

	client.GameRoom = gameRoom

	// ルームにプレイヤー追加
	gameRoom.Mutex.Lock()
	gameRoom.Players[player.ID] = player
	gameRoom.Mutex.Unlock()

	// 初期状態送信
	initMsg := Message{
		Type: "init",
		Data: map[string]interface{}{
			"player":   player,
			"gameRoom": gameRoom.ID,
		},
	}
	if err := ws.WriteJSON(initMsg); err != nil {
		log.Println("初期状態送信エラー:", err)
		return err
	}

	// メッセージ処理ループ
	for {
		var msg Message
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Println("メッセージ読み込みエラー:", err, "クライアントID:", clientID)

			// 切断処理
			gameRoom.Mutex.Lock()
			delete(gameRoom.Players, player.ID)
			gameRoom.Mutex.Unlock()

			clientsMutex.Lock()
			delete(clients, clientID)
			clientsMutex.Unlock()

			break
		}

		// メッセージタイプによる処理分岐
		switch msg.Type {
		case "move":
			if data, ok := msg.Data.(map[string]interface{}); ok {
				if vx, ok := data["vx"].(float64); ok {
					player.VelocityX = vx
				}
				if vy, ok := data["vy"].(float64); ok {
					player.VelocityY = vy
				}
			}
		case "shoot":
			createBullet(gameRoom, player)
		}
	}

	return nil
}

/**
 * 弾の作成
 * プレイヤーの位置から弾を発射する
 * @param {*GameRoom} gameRoom - ゲームルームへのポインタ
 * @param {*Player} player - 弾を発射するプレイヤーへのポインタ
 */
func createBullet(gameRoom *GameRoom, player *Player) {
	bulletID := uuid.New().String()
	bullet := &Entity{
		ID:        bulletID,
		Type:      "bullet",
		X:         player.X + float64(player.Width)/2 - 2.5, // プレイヤーの中央から発射
		Y:         player.Y,
		VelocityX: 0,
		VelocityY: -5, // 上方向に発射
		Width:     5,
		Height:    10,
	}

	gameRoom.Mutex.Lock()
	gameRoom.Bullets[bulletID] = bullet
	gameRoom.Mutex.Unlock()
}

/**
 * 敵の作成
 * ランダムな位置と速度で敵を生成する
 * @param {*GameRoom} gameRoom - ゲームルームへのポインタ
 */
func createEnemy(gameRoom *GameRoom) {
	enemyID := uuid.New().String()
	enemy := &Entity{
		ID:        enemyID,
		Type:      "enemy",
		X:         float64(rand.Intn(600)),
		Y:         0,
		VelocityX: float64(rand.Intn(3) - 1),
		VelocityY: float64(rand.Intn(2) + 1),
		Width:     30,
		Height:    30,
	}

	gameRoom.Mutex.Lock()
	gameRoom.Enemies[enemyID] = enemy
	gameRoom.Mutex.Unlock()
}

/**
 * 衝突判定
 * 2つのエンティティの衝突を判定する
 * @param {*Entity} a - エンティティA
 * @param {*Entity} b - エンティティB
 * @returns {bool} - 衝突している場合true
 */
func checkCollision(a, b *Entity) bool {
	return a.X < b.X+float64(b.Width) &&
		a.X+float64(a.Width) > b.X &&
		a.Y < b.Y+float64(b.Height) &&
		a.Y+float64(a.Height) > b.Y
}

/**
 * ゲームループ
 * 一定間隔でゲーム状態を更新し、クライアントに送信する
 * @param {*GameRoom} gameRoom - ゲームルームへのポインタ
 */
func gameLoop(gameRoom *GameRoom) {
	ticker := time.NewTicker(time.Second / 60)     // 60FPS
	enemyTicker := time.NewTicker(time.Second * 2) // 2秒ごとに敵生成

	defer ticker.Stop()
	defer enemyTicker.Stop()

	for {
		select {
		case <-ticker.C:
			updateGame(gameRoom)
			broadcastGameState(gameRoom)

			// ルームが空なら終了
			if len(gameRoom.Players) == 0 {
				gamesMutex.Lock()
				delete(gameRooms, gameRoom.ID)
				gamesMutex.Unlock()
				log.Println("空のゲームルームを削除しました:", gameRoom.ID)
				return
			}

		case <-enemyTicker.C:
			createEnemy(gameRoom)
		}
	}
}

/**
 * ゲーム状態更新
 * エンティティの移動や衝突判定などのゲームロジックを処理する
 * @param {*GameRoom} gameRoom - ゲームルームへのポインタ
 */
func updateGame(gameRoom *GameRoom) {
	gameRoom.Mutex.Lock()
	defer gameRoom.Mutex.Unlock()

	// プレイヤー移動
	for _, player := range gameRoom.Players {
		player.X += player.VelocityX
		player.Y += player.VelocityY

		// 画面端の衝突判定
		if player.X < 0 {
			player.X = 0
		}
		if player.X > 770 {
			player.X = 770
		}
		if player.Y < 0 {
			player.Y = 0
		}
		if player.Y > 570 {
			player.Y = 570
		}
	}

	// 弾の移動
	for id, bullet := range gameRoom.Bullets {
		bullet.X += bullet.VelocityX
		bullet.Y += bullet.VelocityY

		// 画面外に出たら削除
		if bullet.Y < 0 || bullet.Y > 600 || bullet.X < 0 || bullet.X > 800 {
			delete(gameRoom.Bullets, id)
			continue
		}

		// 敵との衝突判定
		for enemyID, enemy := range gameRoom.Enemies {
			if checkCollision(bullet, enemy) {
				// 衝突したら敵と弾を削除、スコア加算
				delete(gameRoom.Bullets, id)
				delete(gameRoom.Enemies, enemyID)

				// 弾を発射したプレイヤーにスコア加算
				for _, player := range gameRoom.Players {
					if player.X == bullet.X && player.Y == bullet.Y {
						player.Score += 10
						break
					}
				}
				break
			}
		}
	}

	// 敵の移動
	for id, enemy := range gameRoom.Enemies {
		enemy.X += enemy.VelocityX
		enemy.Y += enemy.VelocityY

		// 画面外に出たら削除
		if enemy.Y > 600 {
			delete(gameRoom.Enemies, id)
			continue
		}

		// プレイヤーとの衝突判定
		for _, player := range gameRoom.Players {
			if checkCollision(enemy, &player.Entity) {
				// 衝突したらダメージ
				player.Health -= 10
				if player.Health <= 0 {
					player.Health = 100
					player.Score -= 50
					if player.Score < 0 {
						player.Score = 0
					}
				}
				delete(gameRoom.Enemies, id)
				break
			}
		}
	}
}

/**
 * ゲーム状態のブロードキャスト
 * 現在のゲーム状態を全プレイヤーに送信する
 * @param {*GameRoom} gameRoom - ゲームルームへのポインタ
 */
func broadcastGameState(gameRoom *GameRoom) {
	gameRoom.Mutex.Lock()
	state := map[string]interface{}{
		"players": gameRoom.Players,
		"bullets": gameRoom.Bullets,
		"enemies": gameRoom.Enemies,
	}
	gameRoom.Mutex.Unlock()

	message := Message{
		Type: "gameState",
		Data: state,
	}

	// 各クライアントに送信
	clientsMutex.Lock()
	for id, client := range clients {
		// このゲームルームに属しているクライアントのみに送信
		if client.GameRoom != nil && client.GameRoom.ID == gameRoom.ID {
			err := client.Socket.WriteJSON(message)
			if err != nil {
				log.Println("ブロードキャストエラー:", err, "クライアントID:", id)
			}
		}
	}
	clientsMutex.Unlock()
}
