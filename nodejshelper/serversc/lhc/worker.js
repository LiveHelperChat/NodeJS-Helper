var SCWorker = require('socketcluster/scworker');
var express = require('express');
var serveStatic = require('serve-static');
var path = require('path');
var morgan = require('morgan');
var healthChecker = require('sc-framework-health-check');
var crypto = require('crypto');

class Worker extends SCWorker {
  run() {
    console.log('   >> Worker PID:', process.pid);
    var environment = this.options.environment;

    var app = express();

    var httpServer = this.httpServer;
    var scServer = this.scServer;

    if (environment === 'dev') {
      // Log every HTTP request. See https://github.com/expressjs/morgan for other
      // available formats.
      app.use(morgan('dev'));
    }
    app.use(serveStatic(path.resolve(__dirname, 'public')));

    // Add GET /health-check express route
    healthChecker.attach(this, app);

    httpServer.on('request', app);

    //var count = 0;

    /*
      In here we handle our incoming realtime connections and listen for events.
    */
    scServer.on('connection', function (socket) {

      socket.on('login', function (token, respond) {
        let buff = new Buffer(token.hash, 'base64');  
        let hash = buff.toString('ascii');
        var signatures = hash.split('.');
            var SHA1 = function(input){
              return crypto.createHash('sha1').update(input).digest('hex');
            }
            if(signatures[0] == 'visitor'){
              var validateHash = 'visitor' + '.' + SHA1(signatures[2]) + '.' + signatures[2] + '.' + SHA1(signatures[2]);
            } else if(signatures[0] == 'operator'){
              var validateHash = 'operator' + '.' + SHA1(signatures[2]) + '.' + signatures[2] + '.' + SHA1(signatures[2]);
            }
            if(validateHash == hash){
              var isValidLogin = true;
            }
          if (isValidLogin) {
            respond();
            var now = new Date (),
                lifeTime = new Date ( now );
                lifeTime.setMinutes ( now.getMinutes() + 30 );
            socket.setAuthToken({token:token.hash, exp: lifeTime.getTime(), chanelName:token.chanelName});
          } else {
            // Passing string as first argument indicates error
            respond('Login failed');
          }
        });

      // Some sample logic to show how to handle client events,
      // replace this with your own logic

      /*socket.on('sampleClientEvent', function (data) {
        count++;
        console.log('Handled sampleClientEvent', data);
        scServer.exchange.publish('sample', count);
      });*/

      /*var interval = setInterval(function () {
        socket.emit('random', {
          number: Math.floor(Math.random() * 5)
        });
      }, 1000);*/

      socket.on('disconnect', function () {
        //clearInterval(interval);
      });
    });

    scServer.addMiddleware(scServer.MIDDLEWARE_PUBLISH_IN, function (req, next) {
      var authToken = req.socket.authToken;

      if (authToken && req.channel == authToken.chanelName) {
        next();
      } else {
        next('You are not authorized to publish to ' + req.channel);
      }
    });
  }
}

new Worker();
