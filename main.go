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
	gin.SetMode(gin.ReleaseMode)
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
	router.GET("/api/version", version)
	router.POST("/api/login", login)
	router.GET("/api/card/:code", searchCard)
	router.POST("/api/card", activateCard)
	router.GET("/api/card/:code/histories", latestHistory)
	router.POST("/api/supply-history", supply)
	router.POST("/api/consume-history", consume)
	router.GET("/api/check-login", checkLoginApiWrapper)
	err := router.Run("0.0.0.0:8086")
	if err != nil {
		return
	}
}

func version(c *gin.Context) {
	c.IndentedJSON(http.StatusOK, gin.H{"version": "20211121"})
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

const passwd = "2b10285dcad70b24121540f8e054ae3e62faa0db63d43734766d958b76f94a49"
func login(c *gin.Context) {
	body, err := c.GetRawData()
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"msg": "bad request"})
		return
	}
	s := hash(hash(string(body)))
	log.Print("input hashed:", s)
	if s == passwd {
		magic = randStringBytesMaskImpSrcUnsafe(32)
		c.SetCookie("magic", magic, 7200, "/", "sunyongfei.cn", false, false)
		c.IndentedJSON(http.StatusOK, gin.H{"msg": "登录成功"})
	} else {
		c.IndentedJSON(http.StatusTeapot, gin.H{"msg": "密码错误"})
	}
}

func checkLoginApiWrapper(c *gin.Context) {
	checkLogin(c)
	return
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

func latestHistory(c *gin.Context) {
	if !checkLogin(c) {
		return
	}
	cardCode := c.Param("code")
	var histories []History
	db.Where("card = ?", cardCode).Order("ID desc").Limit(10).Find(&histories)
	c.IndentedJSON(http.StatusOK, histories)
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
	if len(card.Code) != 6 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"msg": "卡号位数不对，请输入6位"})
		return
	}
	if card.Deposit == 0 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"msg": "金额不能为0元"})
		return
	}
	var existCard Card
	checkResult := db.First(&existCard, "code = ?", card.Code)
	if errors.Is(checkResult.Error, gorm.ErrRecordNotFound) {
		// 开卡
		createResult := db.Create(&Card{Code: card.Code, Deposit: card.Deposit, Phone: card.Phone})
		if createResult.Error == nil {
			// 成功
			history := History{Card: card.Code, Money: card.Deposit}
			db.Create(&history)
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
	if history.Money < 0 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"msg": "金额不能为负"})
		return
	}
	httpStatusCode, msg := updateCard(history, "充值")
	c.IndentedJSON(httpStatusCode, gin.H{"msg": msg})
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
	if history.Money < 0 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"msg": "金额不能为负"})
		return
	}
	history.Money *= -1
	httpStatusCode, msg := updateCard(history, "消费")
	c.IndentedJSON(httpStatusCode, gin.H{"msg": msg})
}

func updateCard(history History, operation string) (int, string) {
	if history.Money == 0 {
		return http.StatusBadRequest, "金额不能为0元"
	}
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
		return http.StatusOK, fmt.Sprintf("%s成功，卡号：%s 剩余 %d 元", operation, card.Code, card.Deposit + history.Money)
	}
}
