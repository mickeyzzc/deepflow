package router

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"server/controller/common"
	"server/controller/model"
	"server/controller/service"
)

func VtapRouter(e *gin.Engine) {
	e.GET("/v1/vtaps/:lcuuid/", getVtap)
	e.GET("/v1/vtaps/", getVtaps)
	e.PATCH("/v1/vtaps/:lcuuid/", updateVtap)
	e.PATCH("/v1/vtaps-by-name/:name/", updateVtap)
	e.PATCH("/v1/vtaps/batch/", batchUpdateVtap)
	e.PATCH("/v1/vtaps-license-type/:lcuuid/", updateVtapLicenseType)
	e.PATCH("/v1/vtaps-license-type/", batchUpdateVtapLicenseType)
	e.DELETE("/v1/vtaps/:lcuuid/", deleteVtap)
	e.DELETE("/v1/vtaps/batch/", batchDeleteVtap)
	e.POST("/v1/rebalance-vtap/", rebalanceVtap)
}

func getVtap(c *gin.Context) {
	args := make(map[string]interface{})
	args["lcuuid"] = c.Param("lcuuid")
	data, err := service.GetVtaps(args)
	JsonResponse(c, data, err)
}

func getVtaps(c *gin.Context) {
	args := make(map[string]interface{})
	args["names"] = c.QueryArray("name")
	if value, ok := c.GetQuery("type"); ok {
		args["type"] = value
	}
	if value, ok := c.GetQuery("vtap_group_lcuuid"); ok {
		args["vtap_group_lcuuid"] = value
	}
	if value, ok := c.GetQuery("controller_ip"); ok {
		args["controller_ip"] = value
	}
	if value, ok := c.GetQuery("analyzer_ip"); ok {
		args["analyzer_ip"] = value
	}
	data, err := service.GetVtaps(args)
	JsonResponse(c, data, err)
}

func updateVtap(c *gin.Context) {
	var err error
	var vtapUpdate model.VtapUpdate

	// 参数校验
	err = c.ShouldBindBodyWith(&vtapUpdate, binding.JSON)
	if err != nil {
		BadRequestResponse(c, common.INVALID_PARAMETERS, err.Error())
		return
	}

	// 接收参数
	// 避免struct会有默认值，这里转为map作为函数入参
	patchMap := map[string]interface{}{}
	c.ShouldBindBodyWith(&patchMap, binding.JSON)

	lcuuid := c.Param("lcuuid")
	name := c.Param("name")
	data, err := service.UpdateVtap(lcuuid, name, patchMap)
	JsonResponse(c, data, err)
}

func batchUpdateVtap(c *gin.Context) {
	var err error

	// 参数校验
	vtapUpdateList := make(map[string][]model.VtapUpdate)
	err = c.ShouldBindBodyWith(&vtapUpdateList, binding.JSON)
	if err != nil {
		BadRequestResponse(c, common.INVALID_PARAMETERS, err.Error())
		return
	}

	// 接收参数
	updateMap := make(map[string]([]map[string]interface{}))
	c.ShouldBindBodyWith(&updateMap, binding.JSON)

	// 参数校验
	if _, ok := updateMap["DATA"]; !ok {
		BadRequestResponse(c, common.INVALID_PARAMETERS, "No DATA in request body")
	}

	data, err := service.BatchUpdateVtap(updateMap["DATA"])
	JsonResponse(c, data, err)
}

func updateVtapLicenseType(c *gin.Context) {
	var err error
	var vtapUpdate model.VtapUpdate

	// 参数校验
	err = c.ShouldBindBodyWith(&vtapUpdate, binding.JSON)
	if err != nil {
		BadRequestResponse(c, common.INVALID_PARAMETERS, err.Error())
		return
	}

	// 接收参数
	// 避免struct会有默认值，这里转为map作为函数入参
	patchMap := map[string]interface{}{}
	c.ShouldBindBodyWith(&patchMap, binding.JSON)

	lcuuid := c.Param("lcuuid")
	data, err := service.UpdateVtapLicenseType(lcuuid, patchMap)
	JsonResponse(c, data, err)
}

func batchUpdateVtapLicenseType(c *gin.Context) {
	var err error

	// 参数校验
	vtapUpdateList := make(map[string][]model.VtapUpdate)
	err = c.ShouldBindBodyWith(&vtapUpdateList, binding.JSON)
	if err != nil {
		BadRequestResponse(c, common.INVALID_PARAMETERS, err.Error())
		return
	}

	// 接收参数
	updateMap := make(map[string]([]map[string]interface{}))
	c.ShouldBindBodyWith(&updateMap, binding.JSON)

	// 参数校验
	if _, ok := updateMap["DATA"]; !ok {
		BadRequestResponse(c, common.INVALID_PARAMETERS, "No DATA in request body")
	}

	data, err := service.BatchUpdateVtapLicenseType(updateMap["DATA"])
	JsonResponse(c, data, err)
}

func deleteVtap(c *gin.Context) {
	var err error

	lcuuid := c.Param("lcuuid")
	data, err := service.DeleteVtap(lcuuid)
	JsonResponse(c, data, err)
}

func batchDeleteVtap(c *gin.Context) {
	var err error

	// 接收参数
	deleteMap := make(map[string][](map[string]string))
	c.ShouldBindBodyWith(&deleteMap, binding.JSON)

	// 参数校验
	if _, ok := deleteMap["DATA"]; !ok {
		BadRequestResponse(c, common.INVALID_PARAMETERS, "No DATA in request body")
		return
	}

	data, err := service.BatchDeleteVtap(deleteMap["DATA"])
	JsonResponse(c, data, err)
}

func rebalanceVtap(c *gin.Context) {
	args := make(map[string]interface{})
	args["check"] = false
	if value, ok := c.GetQuery("check"); ok {
		args["check"] = (strings.ToLower(value) == "true")
	}
	if value, ok := c.GetQuery("type"); ok {
		args["type"] = value
		if args["type"] != "controller" && args["type"] != "analyzer" {
			BadRequestResponse(
				c, common.INVALID_PARAMETERS,
				fmt.Sprintf("type (%s) is not supported", args["type"]),
			)
			return
		}
	} else {
		BadRequestResponse(c, common.INVALID_PARAMETERS, "must specify type")
		return
	}
	data, err := service.VTapRebalance(args)
	JsonResponse(c, data, err)
}
