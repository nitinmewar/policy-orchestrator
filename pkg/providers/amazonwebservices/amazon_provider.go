package amazonwebservices

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/hexa-org/policy-orchestrator/pkg/orchestrator/provider"
	"log"
	"strings"
)

type CognitoClient interface {
	ListUserPools(ctx context.Context, params *cognitoidentityprovider.ListUserPoolsInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ListUserPoolsOutput, error)
	ListUsers(ctx context.Context, params *cognitoidentityprovider.ListUsersInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ListUsersOutput, error)
	AdminEnableUser(ctx context.Context, params *cognitoidentityprovider.AdminEnableUserInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.AdminEnableUserOutput, error)
	AdminDisableUser(ctx context.Context, params *cognitoidentityprovider.AdminDisableUserInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.AdminDisableUserOutput, error)
}

type AmazonProvider struct {
	Client CognitoClient
}

func (a *AmazonProvider) Name() string {
	return "amazon"
}

func (a *AmazonProvider) DiscoverApplications(info provider.IntegrationInfo) ([]provider.ApplicationInfo, error) {
	err := a.ensureClientIsAvailable(info)
	if err != nil {
		return nil, err
	}

	if strings.EqualFold(info.Name, a.Name()) {
		return a.ListUserPools()
	}
	return []provider.ApplicationInfo{}, nil
}

func (a *AmazonProvider) ListUserPools() (apps []provider.ApplicationInfo, err error) {
	poolsInput := cognitoidentityprovider.ListUserPoolsInput{MaxResults: 20}
	pools, err := a.Client.ListUserPools(context.Background(), &poolsInput)
	if err != nil {
		return nil, err
	}
	for _, p := range pools.UserPools {
		apps = append(apps, provider.ApplicationInfo{
			ObjectID:    aws.ToString(p.Id),
			Name:        aws.ToString(p.Name),
			Description: "Cognito identity provider user pool",
		})
	}
	return apps, err
}

func (a *AmazonProvider) GetPolicyInfo(integrationInfo provider.IntegrationInfo, applicationInfo provider.ApplicationInfo) ([]provider.PolicyInfo, error) {
	err := a.ensureClientIsAvailable(integrationInfo)
	if err != nil {
		return nil, err
	}

	filter := "status=\"Enabled\""
	userInput := cognitoidentityprovider.ListUsersInput{UserPoolId: &applicationInfo.ObjectID, Filter: &filter}
	users, err := a.Client.ListUsers(context.Background(), &userInput)
	if err != nil {
		return nil, err
	}
	authenticatedUsers := a.authenticatedUsersFrom(users)

	var policies []provider.PolicyInfo
	policies = append(policies, provider.PolicyInfo{
		Version: "0.3",
		Action:  "Access", // todo - not sure what this should be just yet.
		Subject: provider.SubjectInfo{AuthenticatedUsers: authenticatedUsers},
		Object:  provider.ObjectInfo{Resources: []string{applicationInfo.ObjectID}},
	})
	return policies, nil
}

func (a *AmazonProvider) SetPolicyInfo(integrationInfo provider.IntegrationInfo, applicationInfo provider.ApplicationInfo, policyInfo provider.PolicyInfo) error {
	err := a.ensureClientIsAvailable(integrationInfo)
	if err != nil {
		return err
	}

	var newUsers []string
	for _, user := range policyInfo.Subject.AuthenticatedUsers {
		newUsers = append(newUsers, user)
	}

	filter := "status=\"Enabled\""
	userInput := cognitoidentityprovider.ListUsersInput{UserPoolId: &applicationInfo.ObjectID, Filter: &filter}
	users, listUsersErr := a.Client.ListUsers(context.Background(), &userInput)
	if listUsersErr != nil {
		log.Println("Unable to find amazon cognito users.")
		return listUsersErr
	}
	existingUsers := a.authenticatedUsersFrom(users)

	enableErr := a.EnableUsers(applicationInfo.ObjectID, a.ShouldEnable(existingUsers, newUsers))
	if enableErr != nil {
		log.Println("Unable to enable amazon cognito users.")
		return enableErr
	}

	disable := a.DisableUsers(applicationInfo.ObjectID, a.ShouldDisable(existingUsers, newUsers))
	if disable != nil {
		log.Println("Unable to disable amazon cognito users.")
		return disable
	}
	return nil
}

func (a *AmazonProvider) authenticatedUsersFrom(users *cognitoidentityprovider.ListUsersOutput) []string {
	var authenticatedUsers []string
	for _, u := range users.Users {
		for _, attr := range u.Attributes {
			if aws.ToString(attr.Name) == "email" {
				authenticatedUsers = append(authenticatedUsers, fmt.Sprintf("%s:%s", aws.ToString(u.Username), aws.ToString(attr.Value)))
			}
		}
	}
	return authenticatedUsers
}

func (a *AmazonProvider) EnableUsers(userPoolId string, shouldEnable []string) error {
	for _, enable := range shouldEnable {
		enable := cognitoidentityprovider.AdminEnableUserInput{UserPoolId: &userPoolId, Username: &strings.Split(enable, ":")[0]}
		_, err := a.Client.AdminEnableUser(context.Background(), &enable)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *AmazonProvider) ShouldEnable(existingUsers []string, desiredUsers []string) []string {
	var shouldEnable []string
	for _, newUser := range desiredUsers {
		var contains = false
		for _, existingUser := range existingUsers {
			if newUser == existingUser {
				contains = true
			}
		}
		if !contains {
			shouldEnable = append(shouldEnable, newUser)
		}
	}
	return shouldEnable
}

func (a *AmazonProvider) DisableUsers(userPoolId string, shouldDisable []string) error {
	for _, disableUser := range shouldDisable {

		disable := cognitoidentityprovider.AdminDisableUserInput{UserPoolId: &userPoolId, Username: &strings.Split(disableUser, ":")[0]}
		_, err := a.Client.AdminDisableUser(context.Background(), &disable)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *AmazonProvider) ShouldDisable(existingUsers []string, desiredUsers []string) []string {
	var shouldDisable []string
	for _, existingUser := range existingUsers {
		var contains = false
		for _, newUser := range desiredUsers {
			if strings.Contains(newUser, existingUser) {
				contains = true
			}
		}
		if !contains {
			shouldDisable = append(shouldDisable, existingUser)
		}
	}
	return shouldDisable
}

type CredentialsInfo struct {
	AccessKeyID     string `json:"accessKeyID"`
	SecretAccessKey string `json:"secretAccessKey"`
	Region          string `json:"region"`
}

func (a *AmazonProvider) Credentials(key []byte) CredentialsInfo {
	var foundCredentials CredentialsInfo
	_ = json.NewDecoder(bytes.NewReader(key)).Decode(&foundCredentials)
	return foundCredentials
}

func (a *AmazonProvider) ensureClientIsAvailable(info provider.IntegrationInfo) error {
	foundCredentials := a.Credentials(info.Key)
	if a.Client == nil {
		defaultConfig, err := config.LoadDefaultConfig(context.Background(),
			config.WithCredentialsProvider(credentials.StaticCredentialsProvider{
				Value: aws.Credentials{AccessKeyID: foundCredentials.AccessKeyID, SecretAccessKey: foundCredentials.SecretAccessKey},
			}),
			config.WithRegion(foundCredentials.Region),
		)
		if err != nil {
			return err
		}
		a.Client = cognitoidentityprovider.NewFromConfig(defaultConfig)
	}
	return nil
}