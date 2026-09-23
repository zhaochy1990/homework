package media

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	sts "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sts/v20180813"
	"github.com/tencentyun/cos-go-sdk-v5"
)

// newTencentIssuer 用永久密钥调 STS GetFederationToken；每次签发一枚限定单对象的临时密钥。
func newTencentIssuer(cfg Config) (Issuer, error) {
	client, err := sts.NewClient(
		common.NewCredential(cfg.SecretID, cfg.SecretKey),
		cfg.Region,
		profile.NewClientProfile(),
	)
	if err != nil {
		return nil, err
	}
	return &tencentIssuer{client: client}, nil
}

type tencentIssuer struct{ client *sts.Client }

func (i *tencentIssuer) Issue(ctx context.Context, policy string, ttl time.Duration) (Credentials, error) {
	req := sts.NewGetFederationTokenRequest()
	req.Name = common.StringPtr("homework-upload")
	req.Policy = common.StringPtr(policy)
	req.DurationSeconds = common.Uint64Ptr(uint64(ttl.Seconds()))
	resp, err := i.client.GetFederationTokenWithContext(ctx, req)
	if err != nil {
		return Credentials{}, err
	}
	p := resp.Response
	return Credentials{
		TmpSecretID:  derefString(p.Credentials.TmpSecretId),
		TmpSecretKey: derefString(p.Credentials.TmpSecretKey),
		SessionToken: derefString(p.Credentials.Token),
		ExpiredTime:  time.Unix(int64(derefUint64(p.ExpiredTime)), 0),
	}, nil
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefUint64(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}

// newCOSObjects 构造 COS 对象客户端；HEAD/Delete 用永久密钥，播放 URL 由永久密钥预签名。
func newCOSObjects(cfg Config) (Objects, error) {
	bucketURL, err := url.Parse("https://" + cfg.Bucket + ".cos." + cfg.Region + ".myqcloud.com")
	if err != nil {
		return nil, err
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, &http.Client{
		Transport: &cos.AuthorizationTransport{SecretID: cfg.SecretID, SecretKey: cfg.SecretKey},
	})
	return &cosObjects{client: client, secretID: cfg.SecretID, secretKey: cfg.SecretKey}, nil
}

type cosObjects struct {
	client              *cos.Client
	secretID, secretKey string
}

func (o *cosObjects) Head(ctx context.Context, key string) (int64, error) {
	resp, err := o.client.Object.Head(ctx, key, nil)
	if err != nil {
		if cos.IsNotFoundError(err) {
			return 0, ErrObjectNotFound
		}
		return 0, err
	}
	return resp.ContentLength, nil
}

func (o *cosObjects) Delete(ctx context.Context, key string) error {
	if _, err := o.client.Object.Delete(ctx, key); err != nil {
		if cos.IsNotFoundError(err) {
			return ErrObjectNotFound
		}
		return err
	}
	return nil
}

func (o *cosObjects) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := o.client.Object.GetPresignedURL(ctx, http.MethodGet, key, o.secretID, o.secretKey, ttl, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}
