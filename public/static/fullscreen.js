const videoElementFs = document.getElementById('remoteVideo');
// 获取视频容器，全屏容器而不是视频元素本身，可以避免显示浏览器默认的视频控件
const videoContainer = document.querySelector('.video-container');

// 阻止点击视频时的默认行为（如暂停/播放）
videoElementFs.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
});

// 阻止双击默认行为
videoElementFs.addEventListener('dblclick', (event) => {
    event.preventDefault();
    event.stopPropagation();
});

async function fullscreenPlayer() {
    if (document.documentElement.classList.contains('marble-expanded')) {
        setExpanded(false);
        const exit = document.exitFullscreen || document.webkitExitFullscreen;
        if (exit && (document.fullscreenElement || document.webkitFullscreenElement)) {
            try { await Promise.resolve(exit.call(document)); } catch {}
        }
        document.getElementById('marble-fullscreen').focus({preventScroll:true});
        return;
    }
    const exit = document.exitFullscreen || document.webkitExitFullscreen;
    if (document.fullscreenElement || document.webkitFullscreenElement) {
        if (exit) await Promise.resolve(exit.call(document));
        return;
    }
    // Expand the page rather than the video, so remote input and the restore button stay available.
    window.marbleReleaseTouches?.();
    setExpanded(true);
    // Mobile browsers handle page fullscreen inconsistently. Keep a page-level
    // focus view with a reachable restore button instead of entering video fullscreen.
    if (window.matchMedia('(max-width:650px), (max-height:550px)').matches) {
        showToast('已收起操作列，按右上角可顯示返。', 2500);
        return;
    }
    const target = document.documentElement;
    const request = target.requestFullscreen || target.webkitRequestFullscreen;
    if (request) {
        try { await Promise.resolve(request.call(target)); return; } catch {}
    }
    showToast('已收起操作列，按右上角可顯示返。', 2500);
}

function setExpanded(expanded) {
    document.documentElement.classList.toggle('marble-expanded', expanded);
    document.getElementById('marble-restore-controls').setAttribute('aria-expanded', String(!expanded));
    requestAnimationFrame(() => { if (typeof updateVideoCache === 'function') updateVideoCache(); });
    if (expanded) document.getElementById('marble-restore-controls').focus({preventScroll:true});
}
document.getElementById('marble-restore-controls').addEventListener('click', fullscreenPlayer);
document.addEventListener('keydown', event => {
    if (event.key === 'Escape' && document.documentElement.classList.contains('marble-expanded') && !document.fullscreenElement && !document.webkitFullscreenElement) setExpanded(false);
});

function handleFullscreenChange() {
    const isFullscreen = !!document.fullscreenElement || 
                         !!document.webkitFullscreenElement || 
                         !!document.mozFullScreenElement || 
                         !!document.msFullscreenElement;

    if (isFullscreen) {
        // 样式调整以适应全屏
        // 注意：如果是容器全屏，video 元素的尺寸通常由 CSS (max-width/height: 100%) 自动控制
        // 但我们强制设置一下以防万一
        videoElementFs.style.width = '100%';
        videoElementFs.style.height = '100%';
        videoElementFs.style.objectFit = 'contain';
        
        // 关键：拦截浏览器默认行为
        // touch-action: none 阻止浏览器处理触摸手势（如缩放、滚动）
        videoElementFs.style.touchAction = 'none';
        // overscroll-behavior: none 阻止滚动链和橡皮筋效果（如页面顶部的下拉刷新或边缘导航）
        videoElementFs.style.overscrollBehavior = 'none';
        
    } else {
        setExpanded(false);
        // 退出全屏时恢复
        videoElementFs.style.width = '';
        videoElementFs.style.height = '';
        videoElementFs.style.objectFit = '';
        
        videoElementFs.style.touchAction = '';
        videoElementFs.style.overscrollBehavior = '';
    }
    
    // 触发一次 resize 事件以更新触摸坐标计算缓存
    window.dispatchEvent(new Event('resize'));
}

document.addEventListener('fullscreenchange', handleFullscreenChange);
document.addEventListener('webkitfullscreenchange', handleFullscreenChange);
document.addEventListener('mozfullscreenchange', handleFullscreenChange);
document.addEventListener('MSFullscreenChange', handleFullscreenChange);
