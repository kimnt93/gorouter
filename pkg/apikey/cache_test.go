package apikey

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type cacheRepo struct { repositoryStub; gets int; key entities.ApiKey }
func(r *cacheRepo)GetBySecret(_ context.Context,hash string)(*entities.ApiKey,error){r.gets++;if hash!=r.key.SecretHash{return nil,entities.ErrNotFound};v:=r.key;return &v,nil}
func(r *cacheRepo)Patch(_ context.Context,_ string,_ *bool,_ *[]string,_ *[]string,_ **float64,_ **int)error{return nil}

func TestTokenCacheMissHitSlidingTTLAndInvalidation(t *testing.T){
	m:=miniredis.RunT(t);client:=redis.NewClient(&redis.Options{Addr:m.Addr()});t.Cleanup(func(){_ = client.Close()})
	repo:=&cacheRepo{key:entities.ApiKey{ID:"key_1",SecretHash:"hashed",TenantID:"tenant_1",Name:"cached",Enabled:true}}
	svc:=NewService(repo,func(string)string{return "hashed"},func()string{return "unused"});svc.SetTokenCache(client,10*time.Minute)
	ctx:=context.Background()
	if _,err:=svc.GetBySecret(ctx,"secret");err!=nil{t.Fatal(err)}
	if _,err:=svc.GetBySecret(ctx,"secret");err!=nil{t.Fatal(err)}
	if repo.gets!=1{t.Fatalf("storage lookups=%d, want 1",repo.gets)}
	m.FastForward(9*time.Minute)
	if _,err:=svc.GetBySecret(ctx,"secret");err!=nil{t.Fatal(err)}
	m.FastForward(2*time.Minute)
	if _,err:=svc.GetBySecret(ctx,"secret");err!=nil{t.Fatal(err)}
	if repo.gets!=1{t.Fatalf("sliding TTL missed cache: lookups=%d",repo.gets)}
	enabled:=false
	if err:=svc.Patch(ctx,"key_1",&enabled,nil,nil,nil,nil);err!=nil{t.Fatal(err)}
	if _,err:=svc.GetBySecret(ctx,"secret");err!=nil{t.Fatal(err)}
	if repo.gets!=2{t.Fatalf("patch did not invalidate cache: lookups=%d",repo.gets)}
}

func TestTokenCacheDeleteInvalidates(t *testing.T){
	m:=miniredis.RunT(t);client:=redis.NewClient(&redis.Options{Addr:m.Addr()});defer client.Close()
	repo:=&cacheRepo{key:entities.ApiKey{ID:"key_1",SecretHash:"hashed"}}
	svc:=NewService(repo,func(string)string{return "hashed"},func()string{return "unused"});svc.SetTokenCache(client,time.Minute)
	ctx:=context.Background();_,_ = svc.GetBySecret(ctx,"secret")
	if err:=svc.Delete(ctx,"key_1");err!=nil{t.Fatal(err)}
	_,_ = svc.GetBySecret(ctx,"secret")
	if repo.gets!=2{t.Fatalf("delete did not invalidate cache: lookups=%d",repo.gets)}
}
