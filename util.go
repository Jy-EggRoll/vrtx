package main

import "sync"

// runConcurrent 并发执行一批无返回值的任务，全部完成后返回。
// 用于并行提取各类快捷方式/书签，避免每处重复 waitGroup 样板。
func runConcurrent(tasks ...func()) {
	var wg sync.WaitGroup
	for _, t := range tasks {
		wg.Add(1)
		go func(f func()) {
			defer wg.Done()
			f()
		}(t)
	}
	wg.Wait()
}
