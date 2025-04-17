/**
 * @file main.go
 * @description リアルタイム2Dマルチプレイヤーシューティングゲーム「スペースシューター」のバックエンド
 * @author Claude
 * @version 1.1
 *
 * 概要:
 * - WebSocketを使用したリアルタイム通信
 * - 複数プレイヤーが参加可能なゲームルーム管理
 * - 敵の自動生成と衝突検出
 * - 60FPSでのゲームループ処理
 * - ボス敵の実装
 * - クリア・ゲームオーバー画面
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
 * @property {string} Type - エンティティの種類（"player", "bullet", "enemy", "boss"）
 * @property {float64} X - X座標位置
 * @property {float64} Y - Y座標位置
 * @property {float64} VelocityX - X方向の速度
 * @property {float64} VelocityY - Y方向の速度
 * @property {int} Width - 幅（ピクセル）
 * @property {int} Height - 高さ（ピクセル）
 * @property {int} Health - エンティティの体力（主にボス用）
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
	Health    int     `json:"health"`
}

/**
 * プレイヤー構造体
 * プレイヤー固有の情報を保持
 * @property {Entity} Entity - 基本エンティティ情報（継承）
 * @property {string} Name - プレイヤー名
 * @property {int} Score - スコア
 * @property {int} Health - 体力値
 * @property {string} Color - プレイヤーカラー（16進数カラーコード）
 * @property {int} FirePower - プレイヤーの攻撃力（アイテム取得で増加）
 */
type Player struct {
	Entity
	Name      string `json:"name"`
	Score     int    `json:"score"`
	Health    int    `json:"health"`
	Color     string `json:"color"`
	FirePower int    `json:"firePower"`
}

/**
 * ゲームルーム構造体
 * 一つのゲームインスタンスを表す
 * @property {string} ID - ルームの一意識別子
 * @property {map[string]*Player} Players - プレイヤーマップ（キー：プレイヤーID）
 * @property {map[string]*Entity} Bullets - 弾のマップ（キー：弾ID）
 * @property {map[string]*Entity} Enemies - 敵のマップ（キー：敵ID）
 * @property {*Entity} Boss - ボス敵（存在する場合）
 * @property {map[string]*Entity} Items - アイテムのマップ（キー：アイテムID）
 * @property {time.Time} LastTick - 最後のゲームティック時間
 * @property {sync.Mutex} Mutex - 同時アクセス防止のミューテックス
 * @property {int} EnemiesDefeated - 倒した敵の数
 * @property {bool} BossSpawned - ボスが出現済みかどうか
 * @property {string} GameState - ゲームの状態（"playing", "gameover", "clear"）
 */
type GameRoom struct {
	ID              string             `json:"id"`
	Players         map[string]*Player `json:"players"`
	Bullets         map[string]*Entity `json:"bullets"`
	Enemies         map[string]*Entity `json:"enemies"`
	Boss            *Entity            `json:"boss"`
	Items           map[string]*Entity `json:"items"`
	LastTick        time.Time
	Mutex           sync.Mutex
	EnemiesDefeated int    `json:"enemiesDefeated"`
	BossSpawned     bool   `json:"bossSpawned"`
	GameState       string `json:"gameState"`
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
		ID:              uuid.New().String(),
		Players:         make(map[string]*Player),
		Bullets:         make(map[string]*Entity),
		Enemies:         make(map[string]*Entity),
		Boss:            nil,
		Items:           make(map[string]*Entity),
		LastTick:        time.Now(),
		EnemiesDefeated: 0,
		BossSpawned:     false,
		GameState:       "playing",
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
			Health:    100,
		},
		Name:      "Player-" + clientID[:5],
		Score:     0,
		Health:    100,
		Color:     playerColors[rand.Intn(len(playerColors))],
		FirePower: 1,
	}
	client.Player = player

	// ゲームルーム検索・作成
	gamesMutex.Lock()
	var gameRoom *GameRoom

	// 空きのあるルームを探す
	for _, room := range gameRooms {
		if len(room.Players) < 4 && room.GameState == "playing" { // 最大4人、プレイ中のルームのみ
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
		case "restart":
			// ゲームが終了状態の場合、再スタート
			if gameRoom.GameState == "gameover" || gameRoom.GameState == "clear" {
				gameRoom.Mutex.Lock()
				gameRoom.GameState = "playing"
				gameRoom.EnemiesDefeated = 0
				gameRoom.BossSpawned = false
				gameRoom.Boss = nil
				gameRoom.Enemies = make(map[string]*Entity)
				gameRoom.Bullets = make(map[string]*Entity)

				// プレイヤーの状態をリセット
				for _, p := range gameRoom.Players {
					p.Health = 100
					p.Score = 0
					p.X = float64(300 + rand.Intn(300))
					p.Y = float64(300 + rand.Intn(300))
				}
				gameRoom.Mutex.Unlock()
			}
		}
	}

	return nil
}

/**
 * プレイヤーの FirePower に応じて複数弾を拡散発射
 * @param {*GameRoom} gameRoom - ゲームルームへのポインタ
 * @param {*Player} player - 弾を発射するプレイヤーへのポインタ
 */
func createBullet(gameRoom *GameRoom, player *Player) {
	if gameRoom.GameState != "playing" {
		return
	}
	for i := 0; i < player.FirePower; i++ {
		id := uuid.New().String()
		// 簡易的に左右に拡散させるオフセット
		offset := float64(i-(player.FirePower-1)/2) * 5
		b := &Entity{
			ID:        id,
			Type:      "bullet",
			X:         player.X + float64(player.Width)/2 - 2.5 + offset,
			Y:         player.Y,
			VelocityX: offset * 0.2,
			VelocityY: -6,
			Width:     5,
			Height:    10,
		}
		gameRoom.Mutex.Lock()
		gameRoom.Bullets[id] = b
		gameRoom.Mutex.Unlock()
	}
}

/**
 * 敵の作成
 * ランダムな位置と速度で敵を生成する
 * @param {*GameRoom} gameRoom - ゲームルームへのポインタ
 */
func createEnemy(gameRoom *GameRoom) {
	// ゲームがプレイ中でボスが出現していない場合のみ敵を生成
	if gameRoom.GameState != "playing" || gameRoom.BossSpawned {
		return
	}

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
		Health:    1,
	}

	gameRoom.Mutex.Lock()
	gameRoom.Enemies[enemyID] = enemy
	gameRoom.Mutex.Unlock()
}

/**
 * ボスの作成
 * 画面上部中央に強力なボスを生成する
 * @param {*GameRoom} gameRoom - ゲームルームへのポインタ
 */
func createBoss(gameRoom *GameRoom) {
	boss := &Entity{
		ID:        "boss-" + uuid.New().String(),
		Type:      "boss",
		X:         float64(400 - 50), // 画面中央
		Y:         50,                // 上部
		VelocityX: 2,                 // 左右に移動
		VelocityY: 0,
		Width:     100,
		Height:    80,
		Health:    100, // ボスの体力
	}

	gameRoom.Mutex.Lock()
	gameRoom.Boss = boss
	gameRoom.BossSpawned = true
	gameRoom.Mutex.Unlock()
}

/**
 * 衝突判定（AABB: Axis-Aligned Bounding Box）
 * @param {Entity} a - エンティティA
 * @param {Entity} b - エンティティB
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
			// プレイ中のみ敵を生成
			if gameRoom.GameState == "playing" {
				// 一定数の敵を倒したらボス出現
				if gameRoom.EnemiesDefeated >= 20 && !gameRoom.BossSpawned {
					createBoss(gameRoom)
				} else {
					createEnemy(gameRoom)
				}
			}
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

	// ゲームがプレイ中でない場合は更新しない
	if gameRoom.GameState != "playing" {
		return
	}

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

	// 敵がランダムに撃つ
	for _, enemy := range gameRoom.Enemies {
		if rand.Intn(1000) < 5 { // 確率調整
			bid := uuid.New().String()
			eb := &Entity{
				ID:        bid,
				Type:      "enemyBullet",
				X:         enemy.X + float64(enemy.Width)/2,
				Y:         enemy.Y + float64(enemy.Height),
				VelocityX: 0,
				VelocityY: 3,
				Width:     5,
				Height:    5,
			}
			gameRoom.Bullets[bid] = eb
		}
	}

	// 全弾を移動＆衝突判定
	for id, b := range gameRoom.Bullets {
		b.X += b.VelocityX
		b.Y += b.VelocityY

		// 画面外削除
		if b.Y < 0 || b.Y > 600 || b.X < 0 || b.X > 800 {
			delete(gameRoom.Bullets, id)
			continue
		}

		// 敵／ボス→プレイヤー弾 の当たり判定
		if b.Type == "enemyBullet" || b.Type == "bossBullet" {
			for _, p := range gameRoom.Players {
				if checkCollision(b, &p.Entity) {
					delete(gameRoom.Bullets, id)
					p.Health -= 15
					if p.Health < 0 {
						p.Health = 0
					}
					// 全滅チェック
					allDead := true
					for _, q := range gameRoom.Players {
						if q.Health > 0 {
							allDead = false
							break
						}
					}
					if allDead {
						gameRoom.GameState = "gameover"
					}
					break
				}
			}
			continue
		}

		// プレイヤー弾 の既存処理＋アイテム生成
		if b.Type == "bullet" {
			// ボスとの衝突判定
			if gameRoom.Boss != nil && checkCollision(b, gameRoom.Boss) {
				// 衝突したら弾を削除、ボスにダメージ
				delete(gameRoom.Bullets, id)
				gameRoom.Boss.Health -= 1

				// ボスを倒したらクリア
				if gameRoom.Boss.Health <= 0 {
					gameRoom.GameState = "clear"
					gameRoom.Boss = nil

					// 全プレイヤーにボーナススコア
					for _, player := range gameRoom.Players {
						player.Score += 500
					}
				}
				continue
			}

			// 敵との衝突判定
			for eid, e := range gameRoom.Enemies {
				if checkCollision(b, e) {
					delete(gameRoom.Bullets, id)
					delete(gameRoom.Enemies, eid)
					gameRoom.EnemiesDefeated++

					// 敵倒時にアイテムを落とす
					itemID := uuid.New().String()
					it := &Entity{
						ID:        itemID,
						Type:      "item",
						X:         e.X,
						Y:         e.Y,
						VelocityX: 0,
						VelocityY: 1,
						Width:     15,
						Height:    15,
						Health:    0,
					}
					gameRoom.Items[itemID] = it

					// スコア加算
					for _, player := range gameRoom.Players {
						if player.X == b.X && player.Y == b.Y {
							player.Score += 10
							break
						}
					}
					break
				}
			}
			continue
		}
	}

	// アイテム落下＆取得判定
	for iid, it := range gameRoom.Items {
		it.Y += it.VelocityY
		if it.Y > 600 {
			delete(gameRoom.Items, iid)
			continue
		}
		for _, p := range gameRoom.Players {
			if checkCollision(it, &p.Entity) {
				// 取得で発射能力アップ
				p.FirePower++
				delete(gameRoom.Items, iid)
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
					player.Health = 0

					// 全プレイヤーが死亡したらゲームオーバー
					allPlayersDead := true
					for _, p := range gameRoom.Players {
						if p.Health > 0 {
							allPlayersDead = false
							break
						}
					}

					if allPlayersDead {
						gameRoom.GameState = "gameover"
					}
				}
				delete(gameRoom.Enemies, id)
				break
			}
		}
	}

	// ボスの移動と攻撃
	if gameRoom.Boss != nil {
		// 左右移動
		gameRoom.Boss.X += gameRoom.Boss.VelocityX

		// 画面端で反転
		if gameRoom.Boss.X <= 0 || gameRoom.Boss.X+float64(gameRoom.Boss.Width) >= 800 {
			gameRoom.Boss.VelocityX *= -1
		}

		// ランダムで攻撃（ボスの弾発射）
		if rand.Intn(60) < 5 { // 約1/12の確率で発射
			bulletID := uuid.New().String()
			bullet := &Entity{
				ID:        bulletID,
				Type:      "bossBullet",
				X:         gameRoom.Boss.X + float64(gameRoom.Boss.Width)/2,
				Y:         gameRoom.Boss.Y + float64(gameRoom.Boss.Height),
				VelocityX: float64(rand.Intn(5) - 2), // ランダムな水平速度
				VelocityY: float64(rand.Intn(3) + 2), // 下向きに発射
				Width:     10,
				Height:    10,
			}
			gameRoom.Bullets[bulletID] = bullet
		}

		// プレイヤーとの衝突判定
		for _, player := range gameRoom.Players {
			if checkCollision(gameRoom.Boss, &player.Entity) {
				// 衝突したら大ダメージ
				player.Health -= 20
				if player.Health <= 0 {
					player.Health = 0

					// 全プレイヤーが死亡したらゲームオーバー
					allPlayersDead := true
					for _, p := range gameRoom.Players {
						if p.Health > 0 {
							allPlayersDead = false
							break
						}
					}

					if allPlayersDead {
						gameRoom.GameState = "gameover"
					}
				}
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
		"players":         gameRoom.Players,
		"bullets":         gameRoom.Bullets,
		"enemies":         gameRoom.Enemies,
		"boss":            gameRoom.Boss,
		"items":           gameRoom.Items,
		"gameState":       gameRoom.GameState,
		"enemiesDefeated": gameRoom.EnemiesDefeated,
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
