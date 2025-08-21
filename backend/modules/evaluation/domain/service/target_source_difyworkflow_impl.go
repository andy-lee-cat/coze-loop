// Copyright (c) 2025 coze-dev Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package service

import (
	"context"

	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/bytedance/gopkg/lang/gptr"

	"github.com/coze-dev/cozeloop/backend/modules/evaluation/conf"
	"github.com/coze-dev/cozeloop/backend/modules/evaluation/domain/consts"
	"github.com/coze-dev/cozeloop/backend/modules/evaluation/domain/entity"
	"github.com/coze-dev/cozeloop/pkg/errorx"
	"github.com/coze-dev/cozeloop/pkg/errorx/errno"
	"github.com/coze-dev/cozeloop/pkg/logs"
	"github.com/coze-dev/cozeloop/pkg/session"
)

func NewDifyWorkflowSourceEvalTargetServiceImpl(configer conf.IConfiger) ISourceEvalTargetOperateService {
	return &DifyWorkflowSourceEvalTargetServiceImpl{
		httpClient: &http.Client{},
		configer:   configer,
	}
}

type DifyWorkflowSourceEvalTargetServiceImpl struct {
	httpClient *http.Client
	configer   conf.IConfiger
}

func (d *DifyWorkflowSourceEvalTargetServiceImpl) EvalType() entity.EvalTargetType {
	return entity.EvalTargetTypeDifyWorkflow
}

func (d *DifyWorkflowSourceEvalTargetServiceImpl) ValidateInput(ctx context.Context, spaceID int64, inputSchema []*entity.ArgsSchema, input *entity.EvalTargetInputData) error {
	// 验证输入，Dify的输入是JSON，我们可以简单验证它是否是合法的JSON
	if inputContent, ok := input.InputFields["input"]; ok && inputContent.Text != nil {
		if !json.Valid([]byte(*inputContent.Text)) {
			return errorx.NewByCode(errno.CommonInvalidParamCode, errorx.WithExtraMsg("dify workflow input must be a valid json string"))
		}
	}
	return nil
}

func (d *DifyWorkflowSourceEvalTargetServiceImpl) Execute(ctx context.Context, spaceID int64, param *entity.ExecuteEvalTargetParam) (*entity.EvalTargetOutputData, entity.EvalTargetRunStatus, error) {
	// 使用BuildBySource方法来构建完整的EvalTarget对象
	completeEvalTarget, err := d.BuildBySource(ctx, spaceID, param.SourceTargetID, param.SourceTargetVersion)
	if err != nil {
		return nil, entity.EvalTargetRunStatusFail, errorx.NewByCode(errno.CommonInvalidParamCode, errorx.WithExtraMsg("failed to build eval target from param"))
	}

	if completeEvalTarget.EvalTargetVersion == nil || completeEvalTarget.EvalTargetVersion.DifyWorkflow == nil {
		return nil, entity.EvalTargetRunStatusFail, errorx.NewByCode(errno.CommonInvalidParamCode, errorx.WithExtraMsg("dify workflow config not found in eval target version"))
	}

	apiKey := completeEvalTarget.EvalTargetVersion.DifyWorkflow.APIKey
	if apiKey == "" {
		return nil, entity.EvalTargetRunStatusFail, errorx.NewByCode(errno.CommonInvalidParamCode, errorx.WithExtraMsg("dify api key is empty"))
	}

	// 从配置中读取Dify Host，如果不存在则使用默认值
	difyHost := d.configer.GetString("evaluation.dify.host")
	if difyHost == "" {
		difyHost = "http://8.130.124.150" // 默认生产环境地址
	}
	url := fmt.Sprintf("%s/v1/workflows/run", difyHost)

	// 2. 准备请求体
	difyInputsJSON := "{}" // 默认空JSON对象
	if inputContent, ok := param.Input.InputFields["input"]; ok && inputContent.Text != nil {
		difyInputsJSON = *inputContent.Text
	}

	difyRequestBody := map[string]interface{}{
		"inputs":         json.RawMessage([]byte(difyInputsJSON)),
		"response_mode":  "blocking",
		"user":           fmt.Sprintf("cozeloop_user_%d", spaceID),
	}
	requestBodyBytes, err := json.Marshal(difyRequestBody)
	if err != nil {
		return nil, entity.EvalTargetRunStatusFail, errorx.Wrapf(err, "failed to marshal dify request body")
	}

	// 3. 执行HTTP请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(requestBodyBytes))
	if err != nil {
		return nil, entity.EvalTargetRunStatusFail, errorx.Wrapf(err, "failed to create http request")
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	logs.CtxInfo(ctx, "[DifyWorkflow] Sending request to %s with body: %s", url, string(requestBodyBytes))

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, entity.EvalTargetRunStatusFail, err
	}
	defer resp.Body.Close()

	responseBodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, entity.EvalTargetRunStatusFail, err
	}

	logs.CtxInfo(ctx, "[DifyWorkflow] Received response with status %d and body: %s", resp.StatusCode, string(responseBodyBytes))

	// 4. 结果转换
	if resp.StatusCode != http.StatusOK {
		// Dify API返回非200状态码，表示请求失败
		return nil, entity.EvalTargetRunStatusFail, fmt.Errorf("dify api returned status %d: %s", resp.StatusCode, string(responseBodyBytes))
	}

	// 定义Dify blocking模式的响应体结构（外层包装）
	var difyResponse struct {
		TaskID        string `json:"task_id"`
		WorkflowRunID string `json:"workflow_run_id"`
		Data struct {
			ID          string          `json:"id"`
			WorkflowID  string          `json:"workflow_id"`
			Status      string          `json:"status"`
			Outputs     json.RawMessage `json:"outputs"`
			Error       string          `json:"error"`
			ElapsedTime float64         `json:"elapsed_time"`
			TotalTokens int             `json:"total_tokens"`
			TotalSteps  int             `json:"total_steps"`
			CreatedAt   int64           `json:"created_at"`
			FinishedAt  int64           `json:"finished_at"`
		} `json:"data"`
	}

	if err := json.Unmarshal(responseBodyBytes, &difyResponse); err != nil {
		// 如果无法解析为标准结构，将整个响应体作为错误返回
		return nil, entity.EvalTargetRunStatusFail, fmt.Errorf("failed to parse dify blocking response: %w. Raw response: %s", err, string(responseBodyBytes))
	}

	if difyResponse.Data.Status != "succeeded" {
		// Workflow内部执行失败
		return nil, entity.EvalTargetRunStatusFail, fmt.Errorf("dify workflow execution failed with status '%s': %s", difyResponse.Data.Status, difyResponse.Data.Error)
	}

	outputStr := string(difyResponse.Data.Outputs)
	if outputStr == "" || outputStr == "null" {
		outputStr = "{}" // 确保输出是一个有效的JSON对象字符串
	}

	outputData := &entity.EvalTargetOutputData{
		OutputFields: map[string]*entity.Content{
			consts.OutputSchemaKey: {
				ContentType: gptr.Of(entity.ContentTypeText),
				Format:      gptr.Of(entity.JSON),
				Text:        &outputStr,
			},
		},
		EvalTargetUsage: &entity.EvalTargetUsage{},
	}

	return outputData, entity.EvalTargetRunStatusSuccess, nil
}

func (d *DifyWorkflowSourceEvalTargetServiceImpl) BuildBySource(ctx context.Context, spaceID int64, sourceTargetID, sourceTargetVersion string, opts ...entity.Option) (*entity.EvalTarget, error) {
	// sourceTargetID 可以是用户为这个Dify配置起的名字，比如 "我的订单处理Workflow"。
	// sourceTargetVersion 就是用户填写的 Dify API Key。
	apiKey := sourceTargetVersion
	if apiKey == "" {
		return nil, errorx.NewByCode(errno.CommonInvalidParamCode, errorx.WithExtraMsg("api key is required"))
	}
	userIDInContext := session.UserIDInCtxOrEmpty(ctx)

	return &entity.EvalTarget{
		SpaceID:        spaceID,
		SourceTargetID: sourceTargetID, // 存用户起的名字
		EvalTargetType: entity.EvalTargetTypeDifyWorkflow,
		EvalTargetVersion: &entity.EvalTargetVersion{
			SpaceID:             spaceID,
			SourceTargetVersion: "v1.0", // Dify的配置没有版本概念，给个固定值
			EvalTargetType:      entity.EvalTargetTypeDifyWorkflow,
			DifyWorkflow: &entity.DifyWorkflow{
				Name:   sourceTargetID,
				APIKey: apiKey,
			},
			InputSchema: []*entity.ArgsSchema{
				{
					Key:                 gptr.Of("input"),
					SupportContentTypes: []entity.ContentType{entity.ContentTypeText},
					JsonSchema:          gptr.Of(`{"type": "string", "description": "Dify Workflow的inputs字段，必须是一个JSON格式的字符串"}`),
				},
			},
			OutputSchema: []*entity.ArgsSchema{
				{
					Key:                 gptr.Of(consts.OutputSchemaKey),
					SupportContentTypes: []entity.ContentType{entity.ContentTypeText},
					JsonSchema:          gptr.Of(`{"type": "string", "description": "Dify Workflow的outputs字段，是一个JSON格式的字符串"}`),
				},
			},
			BaseInfo: &entity.BaseInfo{
				CreatedBy: &entity.UserInfo{UserID: gptr.Of(userIDInContext)},
				UpdatedBy: &entity.UserInfo{UserID: gptr.Of(userIDInContext)},
			},
		},
		BaseInfo: &entity.BaseInfo{
			CreatedBy: &entity.UserInfo{UserID: gptr.Of(userIDInContext)},
			UpdatedBy: &entity.UserInfo{UserID: gptr.Of(userIDInContext)},
		},
	}, nil
}

// ListSource, ListSourceVersion, PackSourceInfo 等方法可以先返回空列表或不执行任何操作，因为我们不从Dify动态拉取列表
func (d *DifyWorkflowSourceEvalTargetServiceImpl) ListSource(ctx context.Context, param *entity.ListSourceParam) ([]*entity.EvalTarget, string, bool, error) {
	return nil, "", false, nil
}

func (d *DifyWorkflowSourceEvalTargetServiceImpl) ListSourceVersion(ctx context.Context, param *entity.ListSourceVersionParam) ([]*entity.EvalTargetVersion, string, bool, error) {
	return nil, "", false, nil
}

func (d *DifyWorkflowSourceEvalTargetServiceImpl) PackSourceInfo(ctx context.Context, spaceID int64, dos []*entity.EvalTarget) error {
	return nil
}

func (d *DifyWorkflowSourceEvalTargetServiceImpl) PackSourceVersionInfo(ctx context.Context, spaceID int64, dos []*entity.EvalTarget) error {
	return nil
}