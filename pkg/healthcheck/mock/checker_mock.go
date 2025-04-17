package mock

// CheckerMock はヘルスチェッカーのモック
type CheckerMock struct {
	CheckFunc func(ip string) error
}

// NewCheckerMock は新しいCheckerMockを作成する
func NewCheckerMock(checkFunc func(ip string) error) *CheckerMock {
	return &CheckerMock{
		CheckFunc: checkFunc,
	}
}

// Check はCheckFuncを呼び出す
func (m *CheckerMock) Check(ip string) error {
	if m.CheckFunc != nil {
		return m.CheckFunc(ip)
	}
	return nil
}
