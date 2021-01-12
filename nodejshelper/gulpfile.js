var gulp = require('gulp'),
gutil    = require('gulp-util'),
uglify   = require('gulp-uglify'),
concat   = require('gulp-concat'),
watch 	 = require('gulp-watch');

gulp.task('js-nodejshelper', function() {
	var stylePath = ['design/nodejshelpertheme/js/socketcluster.js',
	                 'design/nodejshelpertheme/js/customjs.js'];
	return gulp.src(stylePath)
	.pipe(concat('nodejshelper.min.js'))
	.pipe(uglify({preserveComments: 'some'}))
	.pipe(gulp.dest('design/nodejshelpertheme/js'));
});

gulp.task('js-nodejshelper-widget', function() {
	var stylePath = ['design/nodejshelpertheme/js/socketcluster.js',
	                 'design/nodejshelpertheme/js/customjs-widget.js'];
	return gulp.src(stylePath)
	.pipe(concat('nodejshelper.widget.min.js'))
	.pipe(uglify({preserveComments: 'some'}))
	.pipe(gulp.dest('design/nodejshelpertheme/js'));
});

gulp.task('js-nodejshelper-admin', function() {
	var stylePath = ['design/nodejshelpertheme/js/socketcluster.js',
	                 'design/nodejshelpertheme/js/customjs-admin.js'];
	return gulp.src(stylePath)
	.pipe(concat('nodejshelper.admin.min.js'))
	.pipe(uglify({preserveComments: 'some'}))
	.pipe(gulp.dest('design/nodejshelpertheme/js'));
});

gulp.task('default', gulp.series('js-nodejshelper','js-nodejshelper-widget','js-nodejshelper-admin'));