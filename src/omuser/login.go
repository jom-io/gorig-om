package omuser

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/jom-io/gorig/apix"
	"github.com/jom-io/gorig/cache"
	"github.com/jom-io/gorig/global/consts"
	"github.com/jom-io/gorig/global/variable"
	"github.com/jom-io/gorig/mid/tokenx"
	"github.com/jom-io/gorig/utils/errors"
	"golang.org/x/crypto/bcrypt"
	"strings"
	"time"
)

// LoginIn is the input structure for login
type loginCountOut struct {
	Count    int    `json:"count"`
	IP       string `json:"ip"`
	LockTime int64  `json:"lock_time"`
}

func Login(ctx *gin.Context) {
	defer apix.HandlePanic(ctx)
	pwd, e := apix.GetParamType[string](ctx, "pwd", apix.Force)
	if e != nil {
		return
	}
	result, err := LoginByPwd(ctx, pwd)
	apix.HandleData(ctx, consts.CurdSelectFailCode, result, err)
}

func LoginByPwd(ctx *gin.Context, hashPwd string) (sign *string, err *errors.Error) {
	if variable.OMKey == "" {
		return nil, errors.Verify("Connection rejected")
	}
	IP := fmt.Sprintf("%s-%s", "OM", ctx.ClientIP())

	loginErrCount, _ := cache.New[loginCountOut](cache.JSON, "loginErrCount").Get(IP)

	if loginErrCount.Count >= 5 {
		if time.Now().Unix() < loginErrCount.LockTime {
			return nil, errors.Verify(fmt.Sprintf("Connection rejected, please try again after %d minutes", (loginErrCount.LockTime-time.Now().Unix())/60+1))
		}
		loginErrCount.Count = 0
		loginErrCount.LockTime = 0
		_ = cache.New[loginCountOut](cache.JSON, "loginErrCount").Set(IP, loginErrCount, 0)
	}

	now := time.Now().Unix() / 10
	localPwd := fmt.Sprintf("%d%s", now, variable.OMKey)
	if e := bcrypt.CompareHashAndPassword([]byte(hashPwd), []byte(localPwd)); e != nil {
		loginErrCount.Count++
		if loginErrCount.Count >= 5 {
			loginErrCount.LockTime = time.Now().Unix() + 60*10 // lock for 10 minutes
			_ = cache.New[loginCountOut](cache.JSON, "loginErrCount").Set(IP, loginErrCount, 0)
			return nil, errors.Verify(fmt.Sprintf("Connection rejected, please try again after %d minutes", (loginErrCount.LockTime-time.Now().Unix())/60+1))
		} else {
			_ = cache.New[loginCountOut](cache.JSON, "loginErrCount").Set(IP, loginErrCount, 0)
			return nil, errors.Verify(fmt.Sprintf("Login failed, %d attempts left", 5-loginErrCount.Count))
		}
	}

	_ = cache.New[loginCountOut](cache.JSON, "loginErrCount").Del(IP)
	tokens, e := tokenx.Get(tokenx.Jwt, tokenx.Memory).Manager.GenerateAndRecord(ctx, IP, nil, time.Now().Unix()+3600)
	if e != nil {
		return nil, e
	}
	return &tokens, nil
}

func IsOM(userID string) bool {
	return strings.HasPrefix(userID, "OM")
}
