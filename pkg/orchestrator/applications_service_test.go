package orchestrator_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/hexa-org/policy-orchestrator/pkg/databasesupport"
	"github.com/hexa-org/policy-orchestrator/pkg/orchestrator"
	"github.com/hexa-org/policy-orchestrator/pkg/orchestrator/test"
	"github.com/hexa-org/policy-orchestrator/pkg/policysupport"
	"github.com/hexa-org/policy-orchestrator/pkg/testsupport"
	"github.com/stretchr/testify/assert"
)

type applicationsServiceData struct {
	db        *sql.DB
	providers map[string]orchestrator.Provider

	fromApp        string
	toApp          string
	toAppDifferent string
}

func (data *applicationsServiceData) SetUp() {
	data.db, _ = databasesupport.Open("postgres://orchestrator:orchestrator@localhost:5432/orchestrator_test?sslmode=disable")
	_, _ = data.db.Exec(`
delete from applications;
delete from integrations;
insert into integrations (id, name, provider, key) values ('50e00619-9f15-4e85-a7e9-f26d87ea12e7', 'aName', 'noop', 'aKey');
insert into applications (id, integration_id, object_id, name, description) values ('6409776a-367a-483a-a194-5ccf9c4ff210', '50e00619-9f15-4e85-a7e9-f26d87ea12e7', 'anObjectId', 'aName', 'aDescription');
insert into applications (id, integration_id, object_id, name, description) values ('6409776a-367a-483a-a194-5ccf9c4ff211', '50e00619-9f15-4e85-a7e9-f26d87ea12e7', 'anotherObjectId', 'anotherName', 'anotherDescription');

insert into integrations (id, name, provider, key) values ('50e00619-9f15-4e85-a7e9-f26d87ea12e8', 'anotherName', 'azure', 'aKey');
insert into applications (id, integration_id, object_id, name, description) values ('6409776a-367a-483a-a194-5ccf9c4ff212', '50e00619-9f15-4e85-a7e9-f26d87ea12e8', 'andAnotherObjectId', 'andAnotherName', 'andAnotherDescription');
`)
	data.fromApp = "6409776a-367a-483a-a194-5ccf9c4ff210"
	data.toApp = "6409776a-367a-483a-a194-5ccf9c4ff211"
	data.toAppDifferent = "6409776a-367a-483a-a194-5ccf9c4ff212"

	data.providers = make(map[string]orchestrator.Provider)
	data.providers["noop"] = &orchestrator_test.NoopProvider{}
}

func (data *applicationsServiceData) TearDown() {
	_ = data.db.Close()
}

func TestApplicationsService_Apply(t *testing.T) {
	testsupport.WithSetUp(&applicationsServiceData{}, func(data *applicationsServiceData) {
		applicationsGateway := orchestrator.ApplicationsDataGateway{DB: data.db}
		integrationsGateway := orchestrator.IntegrationsDataGateway{DB: data.db}
		applicationsService := orchestrator.ApplicationsService{ApplicationsGateway: applicationsGateway, IntegrationsGateway: integrationsGateway, Providers: data.providers}

		err := applicationsService.Apply(orchestrator.Orchestration{From: data.fromApp, To: data.toApp})
		assert.NoError(t, err)

		badFromApp := applicationsService.Apply(orchestrator.Orchestration{From: "", To: data.toApp})
		assert.Error(t, badFromApp)

		badToApp := applicationsService.Apply(orchestrator.Orchestration{From: data.fromApp, To: ""})
		assert.Error(t, badToApp)

		data.providers["noop"] = &orchestrator_test.NoopProvider{Err: errors.New("oops")}

		providerError := applicationsService.Apply(orchestrator.Orchestration{From: data.fromApp, To: data.toApp})
		assert.Error(t, providerError)
	})
}

func TestApplicationsService_RetainResource(t *testing.T) {
	testsupport.WithSetUp(&applicationsServiceData{}, func(data *applicationsServiceData) {
		applicationsGateway := orchestrator.ApplicationsDataGateway{DB: data.db}
		integrationsGateway := orchestrator.IntegrationsDataGateway{DB: data.db}
		applicationsService := orchestrator.ApplicationsService{ApplicationsGateway: applicationsGateway, IntegrationsGateway: integrationsGateway, Providers: data.providers}

		from := []policysupport.PolicyInfo{
			{policysupport.MetaInfo{Version: "aVersion"}, []policysupport.ActionInfo{{"fromAnAction"}}, policysupport.SubjectInfo{Members: []string{"fromAUser"}}, policysupport.ObjectInfo{
				ResourceID: "fromAnId",
			}},
			{policysupport.MetaInfo{Version: "aVersion"}, []policysupport.ActionInfo{{"fromAnotherAction"}}, policysupport.SubjectInfo{Members: []string{"fromAnotherUser"}}, policysupport.ObjectInfo{
				ResourceID: "fromAnId",
			}},
		}

		to := []policysupport.PolicyInfo{
			{policysupport.MetaInfo{Version: "aVersion"}, []policysupport.ActionInfo{{"toAnAction"}}, policysupport.SubjectInfo{Members: []string{"toAUser"}}, policysupport.ObjectInfo{
				ResourceID: "toAnId",
			}},
			{policysupport.MetaInfo{Version: "aVersion"}, []policysupport.ActionInfo{{"toAnotherAction"}}, policysupport.SubjectInfo{Members: []string{"toAnotherUser"}}, policysupport.ObjectInfo{
				ResourceID: "toAnId",
			}},
		}

		modified, _ := applicationsService.RetainResource(from, to)
		assert.Equal(t, "toAnId", modified[0].Object.ResourceID)
		assert.Equal(t, "fromAnAction", modified[0].Actions[0].ActionUri)
		assert.Equal(t, "fromAUser", modified[0].Subject.Members[0])

		assert.Equal(t, "toAnId", modified[1].Object.ResourceID)
		assert.Equal(t, "fromAnotherAction", modified[1].Actions[0].ActionUri)
		assert.Equal(t, "fromAnotherUser", modified[1].Subject.Members[0])

		toWithDifferentResources := []policysupport.PolicyInfo{
			{policysupport.MetaInfo{Version: "aVersion"}, []policysupport.ActionInfo{{"anotherAction"}}, policysupport.SubjectInfo{Members: []string{"anotherUser"}}, policysupport.ObjectInfo{
				ResourceID: "anotherId",
			}},
			{policysupport.MetaInfo{Version: "aVersion"}, []policysupport.ActionInfo{{"anotherAction"}}, policysupport.SubjectInfo{Members: []string{"anotherUser"}}, policysupport.ObjectInfo{
				ResourceID: "andAnotherId",
			}},
		}
		_, err := applicationsService.RetainResource(from, toWithDifferentResources)
		assert.Error(t, err)
	})
}
