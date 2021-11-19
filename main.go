package main

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"log"
	"net/http"
)

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
	router.GET("/card/:code", searchCard)
	router.POST("/card", activateCard)
	router.POST("/supply-history", supply)
	router.POST("/consume-history", consume)
	err := router.Run("0.0.0.0:8086")
	if err != nil {
		return
	}
}

func searchCard(c *gin.Context) {
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
	var card Card
	if err := c.BindJSON(&card); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"msg": "操作不合法，请检查"})
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
	var history History
	if err := c.BindJSON(&history); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"msg": "操作不合法，请检查"})
	}
	c.IndentedJSON(updateCard(history, "充值"))
}

func consume(c *gin.Context) {
	var history History
	if err := c.BindJSON(&history); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"msg": "操作不合法，请检查"})
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
