package host

var Env envService

type envService struct {
}

func init() {
	Env = envService{}
}

//func (cpu *envService) Cpu(ctx *gin.Context) (int, *errors.Error) {
//
//}
