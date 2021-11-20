package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"log"
	"math/rand"
	"net/http"
	"time"
	"unsafe"
)

var magic string
var db *gorm.DB
func init() {
	var err error
	db, err = gorm.Open(sqlite.Open("main.db"), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
	// 迁移 schema
	err = db.AutoMigrate(&Card{}, &History{})
	if err != nil {
		return
	}
	rand.Seed(time.Now().UnixNano())
	//gin.SetMode(gin.ReleaseMode)
}

type Card struct {
	gorm.Model
	Code		string	`gorm:"uniqueIndex"`
	Phone		string
	Deposit		int64
}

type History struct {
	gorm.Model
	Card		string	`gorm:"index"`
	Money		int64
}

func main() {
	router := gin.Default()
	router.POST("/login", login)
	router.GET("/card/:code", searchCard)
	router.POST("/card", activateCard)
	router.POST("/supply-history", supply)
	router.POST("/consume-history", consume)
	err := router.Run("0.0.0.0:8086")
	if err != nil {
		return
	}
}

func hash(target string) string {
	h := sha256.New()
	h.Write([]byte(target))
	sum := h.Sum(nil)
	result := hex.EncodeToString(sum)
	return result
}
var src = rand.NewSource(time.Now().UnixNano())
const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)
func randStringBytesMaskImpSrcUnsafe(n int) string {
	b := make([]byte, n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return *(*string)(unsafe.Pointer(&b))
}

const passwd = "90cc19dfaaecff2ba9f0512d65123764e3c97d4c7335b7ee5c4a841b864ab007"
func login(c *gin.Context) {
	body, err := c.GetRawData()
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"msg": "bad request"})
		return
	}
	s := hash(string(body))
	if s == passwd {
		magic = randStringBytesMaskImpSrcUnsafe(32)
		c.SetCookie("magic", magic, 7200, "/", "sunyongfei.cn", false, false)
		c.IndentedJSON(http.StatusOK, gin.H{"msg": "登录成功"})
	} else {
		c.IndentedJSON(http.StatusTeapot, gin.H{"msg": "密码错误"})
	}
}

func checkLogin(c *gin.Context) bool {
	cookie, err := c.Cookie("magic")
	if err != nil {
		log.Print(err)
		c.IndentedJSON(http.StatusUnauthorized, gin.H{"msg": "请重新登录"})
		return false
	} else if cookie != magic {
		log.Print("magic:", cookie, " refused.")
		c.IndentedJSON(http.StatusUnauthorized, gin.H{"msg": "请重新登录"})
		return false
	} else {
		return true
	}
}

func searchCard(c *gin.Context) {
	if !checkLogin(c) {
		return
	}
	cardCode := c.Param("code")
	var existCard Card
	checkResult := db.First(&existCard, "code = ?", cardCode)
	if errors.Is(checkResult.Error, gorm.ErrRecordNotFound) {
		// 卡号查不到
		c.IndentedJSON(http.StatusNotFound, gin.H{"msg": fmt.Sprintf("卡号：%s 还未激活", cardCode)})
	} else {
		c.IndentedJSON(http.StatusOK, existCard)
	}
}

func activateCard(c *gin.Context) {
	if !checkLogin(c) {
		return
	}
	var card Card
	if err := c.BindJSON(&card); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"msg": "操作不合法，请检查"})
		return
	}
	var existCard Card
	checkResult := db.First(&existCard, "code = ?", card.Code)
	if errors.Is(checkResult.Error, gorm.ErrRecordNotFound) {
		// 开卡
		createResult := db.Create(&Card{Code: card.Code, Deposit: card.Deposit, Phone: card.Phone})
		if createResult.Error == nil {
			// 成功
			c.IndentedJSON(http.StatusCreated, gin.H{"msg": fmt.Sprintf("开卡成功，卡号：%s，余额：%d", card.Code, card.Deposit)})
		} else {
			// 数据库插入失败
			c.IndentedJSON(http.StatusInternalServerError, createResult.Error)
		}
	} else {
		// 该卡已开
		c.IndentedJSON(http.StatusBadRequest, gin.H{"msg": fmt.Sprintf("开卡失败，卡号：%s 已于 %s 激活过", existCard.Code, existCard.CreatedAt.Format("2006年1月2日"))})
	}
}

func supply(c *gin.Context) {
	if !checkLogin(c) {
		return
	}
	var history History
	if err := c.BindJSON(&history); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"msg": "操作不合法，请检查"})
		return
	}
	c.IndentedJSON(updateCard(history, "充值"))
}

func consume(c *gin.Context) {
	if !checkLogin(c) {
		return
	}
	var history History
	if err := c.BindJSON(&history); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"msg": "操作不合法，请检查"})
		return
	}
	history.Money *= -1
	httpCode, msg := updateCard(history, "消费")
	c.IndentedJSON(httpCode, gin.H{"msg": msg})
}

func updateCard(history History, operation string) (int, string) {
	var card Card
	checkResult := db.First(&card, "code = ?", history.Card)
	if errors.Is(checkResult.Error, gorm.ErrRecordNotFound) {
		// 卡号不存在
		return http.StatusNotFound, fmt.Sprintf("卡号：%s 不存在", history.Card)
	} else {
		if card.Deposit+history.Money < 0 {
			return http.StatusBadRequest, fmt.Sprintf("卡号：%s 余额不足", history.Card)
		}
		log.Printf("card:%s %+d", history.Card, history.Money)
		db.Create(&history)
		db.Model(&card).Update("deposit", gorm.Expr("deposit + ?", history.Money))
		return http.StatusOK, operation + "成功"
	}
}
