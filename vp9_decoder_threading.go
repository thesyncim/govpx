package govpx

type vp9LoopFilterPlane uint8

const (
	vp9LoopFilterPlaneY vp9LoopFilterPlane = iota
	vp9LoopFilterPlaneU
	vp9LoopFilterPlaneV
)

type vp9DecoderLoopFilterJob struct {
	d      *VP9Decoder
	miRows int
	miCols int
	plane  vp9LoopFilterPlane
	ok     bool
}

type vp9DecoderLoopFilterPool struct {
	helperCount int8
	start       [2]chan struct{}
	done        [2]chan struct{}
	exited      [2]chan struct{}
	jobs        [2]vp9DecoderLoopFilterJob
}

func newVP9DecoderLoopFilterPool(threads int) *vp9DecoderLoopFilterPool {
	helpers := threads - 1
	if helpers > 2 {
		helpers = 2
	}
	if helpers <= 0 {
		return nil
	}
	p := &vp9DecoderLoopFilterPool{
		helperCount: int8(helpers),
	}
	for i := 0; i < helpers; i++ {
		p.start[i] = make(chan struct{})
		p.done[i] = make(chan struct{})
		p.exited[i] = make(chan struct{})
		go p.workerLoop(i)
	}
	return p
}

func (p *vp9DecoderLoopFilterPool) workerLoop(worker int) {
	defer close(p.exited[worker])
	for range p.start[worker] {
		job := &p.jobs[worker]
		job.ok = job.d.applyVP9LoopFilterPlane(job.miRows, job.miCols,
			job.plane)
		p.done[worker] <- struct{}{}
	}
}

func (p *vp9DecoderLoopFilterPool) shutdown() {
	if p == nil {
		return
	}
	for i := 0; i < int(p.helperCount); i++ {
		close(p.start[i])
	}
	for i := 0; i < int(p.helperCount); i++ {
		<-p.exited[i]
	}
	for i := range p.jobs {
		p.jobs[i] = vp9DecoderLoopFilterJob{}
	}
	p.helperCount = 0
}

func (d *VP9Decoder) applyVP9LoopFilterThreaded(miRows, miCols int) bool {
	p := d.vp9LoopFilterPool
	if p == nil || p.helperCount <= 0 {
		return d.applyVP9LoopFilterSerial(miRows, miCols)
	}

	helpers := int(p.helperCount)
	p.jobs[0] = vp9DecoderLoopFilterJob{
		d:      d,
		miRows: miRows,
		miCols: miCols,
		plane:  vp9LoopFilterPlaneU,
	}
	p.start[0] <- struct{}{}
	if helpers > 1 {
		p.jobs[1] = vp9DecoderLoopFilterJob{
			d:      d,
			miRows: miRows,
			miCols: miCols,
			plane:  vp9LoopFilterPlaneV,
		}
		p.start[1] <- struct{}{}
	}

	ok := d.applyVP9LoopFilterPlane(miRows, miCols, vp9LoopFilterPlaneY)
	<-p.done[0]
	ok = ok && p.jobs[0].ok
	if helpers > 1 {
		<-p.done[1]
		ok = ok && p.jobs[1].ok
	} else {
		ok = ok && d.applyVP9LoopFilterPlane(miRows, miCols,
			vp9LoopFilterPlaneV)
	}
	return ok
}
