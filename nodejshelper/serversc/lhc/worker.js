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

    var secretHash = this.options.secretHash;

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
        var tokenParts = token.hash.split('.');
        var secNow = Math.round(Date.now()/1000);
        if(tokenParts[1] > (secNow - 60*60)) {
          var SHA1 = function(input) {
            return crypto.createHash('sha1').update(input).digest('hex');
          }
          if(tokenParts[1]) {
            var validateVisitorHash = SHA1(tokenParts[1] + 'Visitor' + secretHash);
            var validateOperatorHash = SHA1(tokenParts[1] + 'Operator' + secretHash);
            var validateCustomHash = SHA1(tokenParts[1] + 'Custom' + secretHash);
          }
          if((tokenParts[0] == validateVisitorHash) || (tokenParts[0] == validateOperatorHash) || (tokenParts[0] == validateCustomHash)) {
            respond();
            socket.setAuthToken({token: token.hash, exp: (secNow + 60*30)});
          } else {
            respond('Login failed');
          }
        } else {
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
        if(authToken) {
          next();
        } else {
          next('You are not authorized to publish to ' + req.channel);
        }
    });

    scServer.addMiddleware(scServer.MIDDLEWARE_SUBSCRIBE, function (req, next) {
      var authToken = req.socket.authToken;
        if(authToken) {
          next();
        } else {
          next('You are not authorized to subscribe to ' + req.channel);
        }
      });
  }
}

new Worker();