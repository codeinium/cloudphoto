package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// Yandex bucket policies also restrict IAM-authorized operations. Preserve existing
// management rules rather than replacing them with an anonymous-read-only policy.
func (a *archive) configurePublicRead(ctx context.Context) error {
	document := map[string]any{"Version": "2012-10-17"}
	var statements []any
	result, err := a.client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(a.bucket)})
	if err != nil {
		var apiErr smithy.APIError
		if !errors.As(err, &apiErr) || (apiErr.ErrorCode() != "NoSuchBucketPolicy" && apiErr.ErrorCode() != "NoSuchPolicy") {
			return err
		}
		creds, err := a.client.Options().Credentials.Retrieve(ctx)
		if err != nil {
			return err
		}
		resources := []string{"arn:aws:s3:::" + a.bucket, "arn:aws:s3:::" + a.bucket + "/*"}
		statements = append(statements,
			map[string]any{"Sid": "CloudphotoKeyManagement", "Effect": "Allow", "Principal": "*", "Action": "s3:*", "Resource": resources,
				"Condition": map[string]any{"StringEquals": map[string]string{"yc:access-key-id": creds.AccessKeyID}}},
			map[string]any{"Sid": "CloudConsoleAccess", "Effect": "Allow", "Principal": "*", "Action": "s3:*", "Resource": resources,
				"Condition": map[string]any{"Bool": map[string]string{"aws:PrincipalIsAWSService": "true"}}},
		)
	} else {
		if err := json.Unmarshal([]byte(aws.ToString(result.Policy)), &document); err != nil {
			return fmt.Errorf("Некорректная политика бакета: %w", err)
		}
		if document == nil {
			return errors.New("Некорректная политика бакета")
		}
		switch value := document["Statement"].(type) {
		case []any:
			statements = value
		case map[string]any:
			statements = []any{value}
		default:
			return errors.New("Некорректные правила политики бакета")
		}
	}
	publicRead := map[string]any{"Sid": "PublicRead", "Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::" + a.bucket + "/*"}
	var merged []any
	publicAdded := false
	for _, statement := range statements {
		if rule, ok := statement.(map[string]any); ok && rule["Sid"] == "PublicRead" {
			if !publicAdded {
				merged = append(merged, publicRead)
				publicAdded = true
			}
			continue
		}
		merged = append(merged, statement)
	}
	if !publicAdded {
		merged = append(merged, publicRead)
	}
	document["Statement"] = merged
	body, err := json.Marshal(document)
	if err != nil {
		return err
	}
	_, err = a.client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: aws.String(a.bucket), Policy: aws.String(string(body))})
	if err != nil {
		return err
	}
	// In Yandex Cloud, bucket READ ACL also covers objects. The policy above
	// then restricts anonymous users to GetObject, keeping listing private.
	_, err = a.client.PutBucketAcl(ctx, &s3.PutBucketAclInput{Bucket: aws.String(a.bucket), ACL: types.BucketCannedACLPublicRead})
	return err
}
