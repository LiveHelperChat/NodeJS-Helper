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

gulp.task('default', ['js-nodejshelper'], function() {
	// Just execute all the tasks	
});