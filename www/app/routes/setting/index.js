import Ember from 'ember';
import config from '../../config/environment';

export default Ember.Route.extend({
  auth: Ember.inject.service('auth'),
  foo:undefined,
  watchFoo: function(){
    console.log('foo changed');
  }.observes('foo'),
  isHex: function() {
    Ember.run.later(this, this.refresh, 10);
  },

  actions: {
    error: function(reason, transition) {
      this.transitionTo('/login');
      console.log(reason + transition);
      return false;
    },
    search(devid) {
      if (!Ember.isEmpty(devid)) {
        var result = devid.match(/\b0x[0-9A-Fa-f]{1,40}|[0-9A-Fa-f]{1,40}\b/g);
        console.log(result,devid);
        if (result) {
          localStorage.setItem('devid',result);
          Ember.run.later(this, this.refresh, 10);
        }
      }
    },
    add() {
      const { devid,subid,amount,allowid } = this.get('controller').getProperties('devid', 'subid', 'amount', 'allowid');
      if (!Ember.isEmpty(devid)) {

        var result = devid.match(/\b0x[0-9A-Fa-f]{40}|[0-9A-Fa-f]{40}\b/g);
        console.log(result,devid);
        if (result) {
          var url = config.APP.ApiUrl + 'api/addsubid';
          return Ember.$.ajax({
            method: 'POST',
            url: url,
            crossDomain: true,
            xhrFields: {
              withCredentials: true
            },
            data: JSON.stringify({
              devid: devid,
              subid: subid,
              amount: amount,
              allowid: allowid,
            })
          }).then(function(data) {
            return data;
          });
        }
      }
    },
    delete(devAddr, subAddr) {
      var res_devid = devAddr.match(/\b0x[0-9A-Fa-f]{40}|[0-9A-Fa-f]{40}\b/g);
      var res_subid = subAddr.match(/\b0x[0-9A-Fa-f]{40}|[0-9A-Fa-f]{40}\b/g);
      console.log(res_devid,devAddr);
      if (res_devid && res_subid) {
        var r = this;
        var url = config.APP.ApiUrl + 'api/delsubid';
        return Ember.$.ajax({
          method: 'post',
          url: url,
          crossDomain: true,
          xhrFields: {
            withCredentials: true
          },
          data: JSON.stringify({
            devid: devAddr,
            subid: subAddr,
          })
        }).then(function(data) {
          Ember.run.later(r, r.refresh, 10);
          return data;
        })
      }
    }
  },


	model: function() {
    var devid = localStorage.getItem('devid');
    if (devid) {
      var url = config.APP.ApiUrl + 'api/devsearch';

      return Ember.$.ajax({
        method: 'post',
        url: url,
        crossDomain: true,
        xhrFields: {
          withCredentials: true
        },
        data: JSON.stringify({
          devid: devid,
        })
      }).then(function(data) {
        return data;
      });
    } else {
      return ;
    }
	},

  beforeModel() {
    if(!this.get('auth').isLoggedIn()) {
        this.transitionTo('login');
    }
  },
});
