package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/btcsuite/btcutil/base58"
	"github.com/go-redis/redis/v8"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/proxy"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

const secondsPerDay = 24 * time.Hour
const defaultRenewal = 1

var rdb *redis.Client
var xlog *log.Logger

func init() {
	xlog = log.New(os.Stderr, "", log.LstdFlags|log.Lshortfile)
}

func main() {
	port := flag.Int("port", 8080, "服务端口")
	domain := flag.String("domain", "", "短链接域名，必填项")
	ttl := flag.Duration("ttl", 180*secondsPerDay, "短链接有效期，默认: 180d。")
	dsn := flag.String("dsn", "redis://127.0.0.1:6379", "Redis连接地址，格式: redis://<user>:<password>@<host>:<port>/<db_number>")
	flag.Parse()

	if *domain == "" {
		flag.Usage()
		xlog.Fatalln("短链接域名为必填项")
	}

	// 初始化 Redis 客户端
	options, err := redis.ParseURL(*dsn)
	if err != nil {
		xlog.Fatalln(err)
	}
	rdb = redis.NewClient(options)

	xlog.Printf("shorturl options, port: %d, domain: %s, ttl: %d, dsn: %s\n", *port, *domain, *ttl, *dsn)
	xlog.Printf("redis options, network: %s, addr: %s, username: %s, password: %s, db: %d\n",
		options.Network, options.Addr, options.Username, options.Password, options.DB)

	// 初始化 Api 服务
	app := fiber.New()
	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		TimeFormat: "2006-01-02 15:04:05",
	}))
	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
	}))

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("服务暂时不可用哟！！！")
	})
	app.Get("/http+", func(c *fiber.Ctx) error {
		longUrl := c.OriginalURL()[1:]
		b58LongUrl := base58.Encode([]byte(longUrl))
		shortUrl := rdb.Get(context.TODO(), genLongKey(b58LongUrl)).Val()
		if shortUrl == "" {
			shortUrl = randStringBytesMaskImprSrc(6)
			rdb.MSet(context.TODO(), genLongKey(b58LongUrl), shortUrl, genShortKey(shortUrl), longUrl)
		}
		xlog.Println("shorten:", shortUrl)
		rdb.Expire(context.TODO(), genLongKey(b58LongUrl), *ttl)
		rdb.Expire(context.TODO(), genShortKey(shortUrl), *ttl)
		return c.SendString(fmt.Sprintf("https://%s/%s", *domain, shortUrl))
	})
	app.Get("/:shortUrl", func(c *fiber.Ctx) error {
		shortUrl := c.Params("shortUrl")
		longUrl := rdb.Get(context.TODO(), genShortKey(shortUrl)).Val()
		if longUrl == "" {
			return c.SendString("短链接不存在或已过期！！！")
		}
		renew(shortUrl, longUrl)
		xlog.Println("redirect:", longUrl)
		return c.Redirect(longUrl, http.StatusMovedPermanently)
	})
	app.Get("/sub/:shortUrl", func(c *fiber.Ctx) error {
		shortUrl := c.Params("shortUrl")
		longUrl := rdb.Get(context.TODO(), genShortKey(shortUrl)).Val()
		if longUrl == "" {
			return c.SendString("短链接不存在或已过期！！！")
		}
		renew(shortUrl, longUrl)
		xlog.Println("sub:", longUrl)
		return proxy.Do(c, longUrl)
	})
	app.Get("/proxy/+", func(c *fiber.Ctx) error {
		targetUrl := c.OriginalURL()[7:]
		xlog.Println("proxy:", targetUrl)
		return proxy.Do(c, targetUrl)
	})

	_ = app.Listen(fmt.Sprintf(":%d", *port))
}

func renew(shortUrl, longUrl string) {
	lock := rdb.SetNX(context.TODO(), genLockKey(shortUrl), 1, defaultRenewal*secondsPerDay).Val()
	if lock {
		ttlDuration := rdb.TTL(context.TODO(), genShortKey(shortUrl)).Val()
		if ttlDuration != -1 {
			rdb.Expire(context.TODO(), genLongKey(base58.Encode([]byte(longUrl))), ttlDuration+defaultRenewal*secondsPerDay)
			rdb.Expire(context.TODO(), genShortKey(shortUrl), ttlDuration+defaultRenewal*secondsPerDay)
		}
	}
}

func genLongKey(key string) string {
	return "shorturl:long:" + key
}

func genShortKey(key string) string {
	return "shorturl:short:" + key
}

func genLockKey(key string) string {
	return "shorturl:lock:" + key
}

const letterBytes = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

var src = rand.NewSource(time.Now().UnixNano())

func randStringBytesMaskImprSrc(n int) string {
	b := make([]byte, n)
	// A rand.Int63() generates 63 random bits, enough for letterIdxMax letters!
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
	return string(b)
}
