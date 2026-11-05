package receiptleaf

import "time"

func timeStep(i int) time.Duration { return time.Duration(i) * time.Second }
